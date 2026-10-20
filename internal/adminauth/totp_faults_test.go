package adminauth

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func TestTOTPWriteFailurePreservesCurrentFactorAndPendingRetry(t *testing.T) {
	sqlite, secrets := authDependencies(t)
	service, err := NewService(ServiceConfig{Store: store.NewAdminStore(sqlite.DB), Secrets: secrets,
		LegacyToken: "bootstrap-secret", Passwords: NewPasswordHasher(PasswordParams{
			MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, nil)})
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := sqlite.DB.Exec(`CREATE TRIGGER reject_factor BEFORE UPDATE OF totp_secret_encrypted ON admin_users
		BEGIN SELECT RAISE(ABORT, 'write failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, replacement.Secret)); err == nil {
		t.Fatal("write failure accepted")
	}
	after, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil || !bytes.Equal(before.TOTPSecretEncrypted, after.TOTPSecretEncrypted) {
		t.Fatalf("old factor changed: %v", err)
	}
	if _, err := sqlite.DB.Exec("DROP TRIGGER reject_factor"); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, replacement.Secret)); err != nil {
		t.Fatalf("pending setup could not retry: %v", err)
	}
}

func TestPendingTOTPRejectsDisabledUserAndCorruptSecret(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginTOTP(t.Context(), &principal); err != nil {
		t.Fatal(err)
	}
	pending := service.pendingTOTP.entries[principal.Session.ID]
	pending.secret = []byte("corrupt")
	service.pendingTOTP.entries[principal.Session.ID] = pending
	if err := service.EnableTOTP(t.Context(), &principal, "123456"); err == nil {
		t.Fatal("corrupt pending secret accepted")
	}
	if _, err := service.CreateUser(t.Context(), &principal, CreateUserRequest{Username: "backup-owner", DisplayName: "Backup",
		Role: model.AdminRoleOwner, Password: pendingTestPassword}); err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.UpdateUser(t.Context(), principal.User.ID, "Owner", model.AdminRoleOwner, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginTOTP(t.Context(), &principal); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("disabled user began setup: %v", err)
	}
}

func TestPendingTOTPEncryptionFailureAndLoginRandomFailure(t *testing.T) {
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	setup, err := service.BeginTOTP(t.Context(), &principal)
	if err != nil {
		t.Fatal(err)
	}
	service.config.Secrets = failingTOTPSecrets{ResourceService: service.config.Secrets}
	if err := service.EnableTOTP(t.Context(), &principal, pendingTestCode(t, service, setup.Secret)); err == nil {
		t.Fatal("failed encryption accepted")
	}
	service.random = authFailingReader{}
	if _, err := service.Login(t.Context(), LoginRequest{Username: "owner", Password: pendingTestPassword}); err == nil {
		t.Fatal("failed session entropy accepted")
	}
	if service.passwordBudget.active != 0 {
		t.Fatal("failed session entropy leaked password admission")
	}
}

type failingTOTPSecrets struct{ secretbox.ResourceService }

func (failingTOTPSecrets) WithWrite(context.Context, func(secretbox.ResourceAccess) error) error {
	return errors.New("injected secret failure")
}
