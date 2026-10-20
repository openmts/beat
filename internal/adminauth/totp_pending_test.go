package adminauth

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/beat/backend/internal/model"
)

const pendingTestPassword = "correct horse battery staple"

func pendingTestOwner(t *testing.T, service *Service) model.AdminPrincipal {
	t.Helper()
	result, err := service.Bootstrap(t.Context(), BootstrapRequest{BootstrapToken: "bootstrap-secret",
		Username: "owner", DisplayName: "Owner", Password: pendingTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	return result.Principal
}

func pendingTestCode(t *testing.T, service *Service, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, service.now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestBeginTOTPRequiresRecentAuthentication(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if _, err := service.BeginTOTP(t.Context(), &principal); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatalf("want recent-auth requirement, got %v", err)
	}
}

func TestTOTPSetupDoesNotChangeCurrentFactorBeforeProof(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	setup, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	user, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(user.TOTPSecretEncrypted) != 0 || user.TOTPEnabled() {
		t.Fatal("begin changed durable TOTP state before proof")
	}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, setup.Secret)); err != nil {
		t.Fatal(err)
	}
	before, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginTOTP(t.Context(), &principal); err != nil {
		t.Fatal(err)
	}
	after, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.TOTPEnabled() || !bytes.Equal(before.TOTPSecretEncrypted, after.TOTPSecretEncrypted) {
		t.Fatal("starting replacement disabled or replaced the current authenticator")
	}
}

func TestPendingTOTPIsBoundToSessionAndExpires(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	setup, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Login(t.Context(), LoginRequest{Username: "owner", Password: pendingTestPassword, IPAddress: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Reauthenticate(t.Context(), &other.Principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableTOTP(t.Context(), &other.Principal, pendingTestCode(t, service, setup.Secret)); err == nil {
		t.Fatal("another session confirmed a setup it did not start")
	}
	now := service.now().Add(6 * time.Minute)
	service.now = func() time.Time { return now }
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, setup.Secret)); err == nil {
		t.Fatal("expired setup enabled an authenticator")
	}
}
