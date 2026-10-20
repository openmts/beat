package adminauth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockedPasswordVerifier struct {
	passwordOperations
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (verifier *blockedPasswordVerifier) Verify(string, string) (bool, error) {
	verifier.calls.Add(1)
	verifier.entered <- struct{}{}
	<-verifier.release
	return false, nil
}

func TestLoginAdmitsAtMostFourConcurrentPasswordChecks(t *testing.T) {
	service := newTestService(t)
	verifier := &blockedPasswordVerifier{passwordOperations: service.config.Passwords,
		entered: make(chan struct{}, passwordConcurrentLimit), release: make(chan struct{})}
	service.config.Passwords = verifier
	var workers sync.WaitGroup
	results := make(chan error, passwordConcurrentLimit)
	for index := range passwordConcurrentLimit {
		workers.Go(func() {
			_, err := service.Login(t.Context(), LoginRequest{Username: fmt.Sprintf("absent-%d", index), IPAddress: fmt.Sprint(index)})
			results <- err
		})
	}
	t.Cleanup(func() { close(verifier.release); workers.Wait() })
	for range passwordConcurrentLimit {
		select {
		case <-verifier.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("password check did not start")
		}
	}
	if _, err := service.Login(t.Context(), LoginRequest{Username: "another", IPAddress: "another"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("want admission rejection before password verification, got %v", err)
	}
	if calls := verifier.calls.Load(); calls != passwordConcurrentLimit {
		t.Fatalf("password checks = %d", calls)
	}
}

func TestAuthenticationBudgetBoundsSourcesAndResets(t *testing.T) {
	budget := newAuthenticationBudget()
	now := time.Now()
	for range passwordRequestsPerSource {
		release, allowed := budget.acquire("source", now)
		if !allowed {
			t.Fatal("budget rejected an allowed request")
		}
		release()
		release()
	}
	if _, allowed := budget.acquire("source", now); allowed {
		t.Fatal("source request budget was bypassed")
	}
	release, allowed := budget.acquire("source", now.Add(passwordBudgetWindow))
	if !allowed {
		t.Fatal("expired source budget did not reset")
	}
	release()
	if budget.active != 0 {
		t.Fatal("releasing twice corrupted active count")
	}
}

func TestAuthenticationBudgetDoesNotEvictActiveSourceLimits(t *testing.T) {
	budget := newAuthenticationBudget()
	now := time.Now()
	for index := range passwordBudgetSources {
		key := sha256.Sum256([]byte(fmt.Sprint(index)))
		budget.entries[key] = authenticationBudgetEntry{start: now}
	}
	if _, allowed := budget.acquire("new", now); allowed {
		t.Fatal("unbounded source map")
	}
	release, allowed := budget.acquire("0", now)
	if !allowed {
		t.Fatal("existing source should remain usable")
	}
	release()
}

func TestAuthenticationAdmissionReleasesAfterErrorsAndCancellation(t *testing.T) {
	service := newClosedAuthService(t)
	if _, err := service.Login(t.Context(), LoginRequest{}); err == nil {
		t.Fatal("closed store accepted login")
	}
	if service.passwordBudget.active != 0 {
		t.Fatal("database failure leaked admission")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.Login(ctx, LoginRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled login: %v", err)
	}
	if service.passwordBudget.active != 0 {
		t.Fatal("cancellation leaked admission")
	}
}

func TestReauthenticationAndBootstrapSharePasswordAdmission(t *testing.T) {
	service := newTestService(t)
	releases := make([]func(), 0, passwordConcurrentLimit)
	for index := range passwordConcurrentLimit {
		release, allowed := service.passwordBudget.acquire(fmt.Sprint(index), service.now())
		if !allowed {
			t.Fatal("could not fill password admission")
		}
		releases = append(releases, release)
	}
	t.Cleanup(func() {
		for _, release := range releases {
			release()
		}
	})
	if _, err := service.Bootstrap(t.Context(), BootstrapRequest{BootstrapToken: "bootstrap-secret"}); !errors.Is(err, ErrLocked) {
		t.Fatalf("bootstrap bypassed admission: %v", err)
	}
	principal := recentOwnerPrincipal()
	if err := service.Reauthenticate(t.Context(), &principal, "password", ""); !errors.Is(err, ErrLocked) {
		t.Fatalf("reauthentication bypassed admission: %v", err)
	}
}
