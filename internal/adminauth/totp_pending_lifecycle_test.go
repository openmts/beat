package adminauth

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPendingTOTPIsInvalidatedAfterSessionRevocation(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	setup, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.RevokeSession(t.Context(), principal.Session.ID, service.now()); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, setup.Secret)); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session confirmed TOTP: %v", err)
	}
	user, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil || user.TOTPEnabled() {
		t.Fatalf("revocation changed current factor: %v", err)
	}
}

func TestPendingTOTPRestartDiscardsOnlyUnconfirmedSetup(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	setup, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, setup.Secret)); err != nil {
		t.Fatal(err)
	}
	before, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(service.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, replacement.Secret)); !errors.Is(err, ErrTOTPSetupExpired) {
		t.Fatalf("restarted service recovered unconfirmed state: %v", err)
	}
	after, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil || !after.TOTPEnabled() || !bytes.Equal(before.TOTPSecretEncrypted, after.TOTPSecretEncrypted) {
		t.Fatalf("restart damaged confirmed factor: %v", err)
	}
}

func TestTOTPConcurrentConfirmationCannotOverwriteAnotherFactor(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	second, err := NewService(service.config)
	if err != nil {
		t.Fatal(err)
	}
	services := []*Service{service, second}
	codes := make([]string, len(services))
	for index, candidate := range services {
		setup, err := candidate.BeginTOTP(t.Context(), &principal)
		if err != nil {
			t.Fatal(err)
		}
		codes[index] = pendingTestCode(t, service, setup.Secret)
	}
	var workers sync.WaitGroup
	results := make(chan error, len(services))
	for index, candidate := range services {
		workers.Go(func() { results <- candidate.EnableTOTP(t.Context(), &principal, codes[index]) })
	}
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrTOTPSetupExpired) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("want one factor activation, got %d", success)
	}
}

func TestPendingTOTPCapacityFailuresAndCleanup(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	for index := range maximumPendingTOTP {
		service.pendingTOTP.entries[fmt.Sprint(index)] = pendingTOTP{userID: "other", expiresAt: service.now().Add(pendingTOTPLifetime)}
	}
	if _, err := service.BeginTOTP(t.Context(), &principal); !errors.Is(err, ErrTOTPSetupCapacity) {
		t.Fatalf("pending capacity: %v", err)
	}
	now := service.now().Add(pendingTOTPLifetime)
	service.now = func() time.Time { return now }
	setup, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(service.pendingTOTP.entries) != 1 {
		t.Fatal("expired pending setup entries remain")
	}
	for range maximumPendingTOTPFailures {
		if err := service.EnableTOTP(t.Context(), &principal, "invalid"); !errors.Is(err, ErrInvalidTOTP) {
			t.Fatal(err)
		}
	}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, setup.Secret)); !errors.Is(err, ErrTOTPSetupExpired) {
		t.Fatalf("failed attempts did not invalidate pending setup: %v", err)
	}
}

func TestPendingTOTPRandomAndPrincipalErrorsPreserveState(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	service.random = authFailingReader{}
	if _, err := service.BeginTOTP(t.Context(), &principal); err == nil {
		t.Fatal("randomness failure accepted")
	}
	if _, err := service.BeginTOTP(t.Context(), nil); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	if err := service.DisableTOTP(t.Context(), nil); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	if err := service.Reauthenticate(t.Context(), nil, "", ""); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	user, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil || len(user.TOTPSecretEncrypted) != 0 {
		t.Fatalf("failed begin changed durable state: %v", err)
	}
	principal.User.ID = "someone-else"
	if _, err := service.BeginTOTP(t.Context(), &principal); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
}

func TestPendingTOTPRejectsExpiredSessionAndRecentProof(t *testing.T) {
	for _, elapsed := range []time.Duration{reauthenticationWindow, sessionAbsoluteLifetime} {
		t.Run(elapsed.String(), func(t *testing.T) {
			service := newTestService(t)
			principal := pendingTestOwner(t, service)
			if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
				t.Fatal(err)
			}
			now := service.now().Add(elapsed)
			service.now = func() time.Time { return now }
			if _, err := service.BeginTOTP(t.Context(), &principal); err == nil {
				t.Fatal("expired session proof accepted")
			}
		})
	}
}
