package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/service"
)

func TestGuardedOperationRejectsCapacityAndStalePrincipal(t *testing.T) {
	router := setupSessionRouter(t)
	owner := boundaryOwner(t, router)
	var releases []func()
	for range 4 {
		_, release, err := router.security.GuardSession(t.Context(), owner.Principal)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
		releases = append(releases, release)
	}
	request := httptest.NewRequest(http.MethodPost, "http://example.com/operation", nil)
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), owner.Principal))
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("rejected operation ran") })
	response := httptest.NewRecorder()
	router.guardedAdminOperation(next).ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("capacity status = %d", response.Code)
	}
	for _, release := range releases {
		release()
	}
	if err := router.security.Logout(t.Context(), &owner.Principal); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	router.guardedAdminOperation(next).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("stale principal status = %d", response.Code)
	}
}

type cancellableBatch struct {
	started chan struct{}
	closed  chan struct{}
	release chan struct{}
}

func (*cancellableBatch) OpenTerminal(context.Context, string, io.ReadWriteCloser) error { return nil }

func (batch *cancellableBatch) ExecuteBatch(ctx context.Context, _ []string, _ string) []service.BatchResult {
	close(batch.started)
	select {
	case <-ctx.Done():
		close(batch.closed)
	case <-batch.release:
	}
	return nil
}

func TestLogoutCancelsBatchExecution(t *testing.T) {
	batch := &cancellableBatch{started: make(chan struct{}), closed: make(chan struct{}), release: make(chan struct{})}
	router := setupSessionRouterWithOptions(t, WithTerminalOperations(batch))
	owner := boundaryOwner(t, router)
	cookie := &http.Cookie{Name: handler.AdminSessionCookieName, Value: owner.Token}
	done := make(chan struct{})
	go func() {
		_ = requestRouterWithCookie(router, http.MethodPost, "/api/v1/terminal/execute",
			`{"node_ids":["test"],"command":"sleep 30"}`, cookie, "http://example.com")
		close(done)
	}()
	t.Cleanup(func() { close(batch.release); <-done })
	select {
	case <-batch.started:
	case <-time.After(time.Second):
		t.Fatal("batch did not start")
	}
	if err := router.security.Logout(t.Context(), &owner.Principal); err != nil {
		t.Fatal(err)
	}
	select {
	case <-batch.closed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("batch continued after session logout")
	}
}
