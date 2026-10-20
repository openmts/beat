package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
)

type fakeApplicationSecrets struct{ err error }

func (fakeApplicationSecrets) Status() applicationsecrets.Status {
	return applicationsecrets.Status{Healthy: true, Generation: 2, Rotation: "idle"}
}

func (fake fakeApplicationSecrets) StartOwnerRotation(context.Context, applicationsecrets.RotationActor) (applicationsecrets.Status, error) {
	return fake.Status(), fake.err
}

func TestApplicationSecretHandlersDoNotExposeErrors(t *testing.T) {
	for name, err := range map[string]error{"ok": nil, "busy": applicationsecrets.ErrLifecycleBusy, "limit": applicationsecrets.ErrKeyLimit, "failure": errors.New("SENSITIVE-DATA"),
		"forbidden": applicationsecrets.ErrRotationForbidden, "recent": applicationsecrets.ErrRotationRecentAuth} {
		t.Run(name, func(t *testing.T) {
			handler := NewApplicationSecretHandler(fakeApplicationSecrets{err: err})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{}))
			handler.HandleRotate(response, request)
			expected := http.StatusAccepted
			if name == "busy" || name == "limit" {
				expected = http.StatusConflict
			} else if err != nil {
				expected = http.StatusServiceUnavailable
			}
			if name == "forbidden" {
				expected = http.StatusForbidden
			}
			if name == "recent" {
				expected = http.StatusPreconditionRequired
			}
			if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "SENSITIVE-DATA") {
				t.Fatalf("unsafe rotation response: %d %s", response.Code, response.Body.String())
			}
		})
	}
	response := httptest.NewRecorder()
	NewApplicationSecretHandler(fakeApplicationSecrets{}).HandleStatus(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"generation":2`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret status missing or cacheable")
	}
	response = httptest.NewRecorder()
	NewApplicationSecretHandler(fakeApplicationSecrets{}).HandleRotate(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unauthenticated rotation accepted")
	}
}
