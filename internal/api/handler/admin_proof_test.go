package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/adminauth"
)

func proofTestLogin(t *testing.T, security *adminauth.Service) adminauth.LoginResult {
	t.Helper()
	result, err := security.Bootstrap(t.Context(), adminauth.BootstrapRequest{BootstrapToken: "bootstrap",
		Username: "owner", DisplayName: "Owner", Password: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFailedIdentityProofDoesNotInvalidateAnActiveSession(t *testing.T) {
	security := newHandlerSecurity(t)
	login := proofTestLogin(t, security)
	handler := NewAdminAuthHandler(security)
	response := callAdminHandler(t, &login.Principal, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"wrong","totp_code":""}`, handler.HandleReauthenticate)
	if response.Code != http.StatusForbidden {
		t.Fatalf("want 403 proof failure, got %d", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("proof response is cacheable")
	}
	if _, err := security.Authenticate(t.Context(), login.Token); err != nil {
		t.Fatalf("valid session was invalidated: %v", err)
	}
	for _, secret := range []string{login.Token, "owner", "wrong"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("identity proof error disclosed supplied information")
		}
	}
}

func TestIncorrectSetupCodeReturnsProofFailureNotInvalidSession(t *testing.T) {
	security := newHandlerSecurity(t)
	login := proofTestLogin(t, security)
	if err := security.Reauthenticate(t.Context(), &login.Principal, "correct horse battery staple", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := security.BeginTOTP(t.Context(), &login.Principal); err != nil {
		t.Fatal(err)
	}
	handler := NewAdminAuthHandler(security)
	response := callAdminHandler(t, &login.Principal, http.MethodPost, "/api/v1/admin/users/me/totp",
		`{"code":"invalid"}`, handler.HandleTOTP)
	if response.Code != http.StatusForbidden {
		t.Fatalf("want 403 proof failure, got %d", response.Code)
	}
	if _, err := security.Authenticate(t.Context(), login.Token); err != nil {
		t.Fatalf("valid session was invalidated: %v", err)
	}
}

func TestTOTPBoundaryErrorsAreActionableWithoutSecrets(t *testing.T) {
	for _, test := range []struct {
		failure  error
		expected int
	}{
		{adminauth.ErrTOTPSetupExpired, http.StatusConflict},
		{adminauth.ErrTOTPSetupCapacity, http.StatusTooManyRequests},
		{adminauth.ErrSessionInvalid, http.StatusUnauthorized},
		{adminauth.ErrLocked, http.StatusTooManyRequests},
	} {
		response := httptest.NewRecorder()
		writeIdentityProofError(response, test.failure)
		if response.Code != test.expected {
			t.Fatalf("want %d, got %d", test.expected, response.Code)
		}
		if test.expected == http.StatusTooManyRequests && response.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry guidance")
		}
	}
}
