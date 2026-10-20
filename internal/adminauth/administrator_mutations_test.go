package adminauth

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type administratorHashInterleave struct {
	passwordOperations
	before func()
}

func (r *administratorHashInterleave) Hash(password string) (string, error) {
	if r.before != nil {
		callback := r.before
		r.before = nil
		callback()
	}
	return r.passwordOperations.Hash(password)
}

func administratorServiceOwner(t *testing.T) (*Service, model.AdminPrincipal) {
	t.Helper()
	service := newTestService(t)
	principal := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &principal, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	return service, principal
}

func TestAdministratorCreateRechecksProofAfterHash(t *testing.T) {
	for _, kind := range []string{"session", "recent", "role"} {
		t.Run(kind, func(t *testing.T) {
			service, owner := administratorServiceOwner(t)
			expected := ErrSessionInvalid
			if kind == "recent" {
				expected = ErrRecentAuthRequired
			}
			if kind == "role" {
				expected = ErrForbidden
			}
			backup := owner.User
			backup.ID = "backup"
			backup.Username = "backup"
			if err := service.config.Store.CreateUser(t.Context(), &backup); err != nil {
				t.Fatal(err)
			}
			service.config.Passwords = &administratorHashInterleave{passwordOperations: service.config.Passwords, before: func() {
				var err error
				switch kind {
				case "session":
					err = service.config.Store.RevokeSession(t.Context(), owner.Session.ID, service.Now())
				case "recent":
					err = service.config.Store.MarkSessionReauthenticated(t.Context(), owner.Session.ID, service.Now().Add(-time.Second))
				case "role":
					err = service.config.Store.UpdateUser(t.Context(), owner.User.ID, owner.User.DisplayName, model.AdminRoleAdmin, true)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			_, err := service.CreateUser(t.Context(), &owner, CreateUserRequest{Username: "target", DisplayName: "Target", Role: model.AdminRoleAdmin, Password: "a new secure account password"})
			if !errors.Is(err, expected) {
				t.Fatalf("want %v, got %v", expected, err)
			}
			user, err := service.config.Store.GetUserByUsername(t.Context(), "target")
			if err != nil || user != nil {
				t.Fatalf("unauthorized creation: %+v %v", user, err)
			}
		})
	}
}

func TestAdministratorPasswordSnapshotInterleave(t *testing.T) {
	service, owner := administratorServiceOwner(t)
	service.config.Passwords = &administratorHashInterleave{passwordOperations: service.config.Passwords, before: func() {
		if err := service.config.Store.UpdatePassword(t.Context(), owner.User.ID, "concurrently-committed-hash", service.Now()); err != nil {
			t.Fatal(err)
		}
	}}
	err := service.ChangePassword(t.Context(), &owner, pendingTestPassword, "replacement password long", "")
	if !errors.Is(err, store.ErrAdminMutationConflict) {
		t.Fatalf("stale password result: %v", err)
	}
	current, err := service.config.Store.GetUserByID(t.Context(), owner.User.ID)
	if err != nil || current.PasswordHash != "concurrently-committed-hash" {
		t.Fatalf("new password overwritten: %+v %v", current, err)
	}
	page, err := service.config.Store.ListAuditEvents(t.Context(), model.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Action == store.AdministratorPassword && event.Outcome == model.AuditOutcomeSuccess {
			t.Fatal("stale replacement created success audit")
		}
	}
}

func TestAdministratorHashFailuresDoNotChangeAccounts(t *testing.T) {
	service, owner := administratorServiceOwner(t)
	service.config.Passwords = NewPasswordHasher(PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, authFailingReader{})
	if _, err := service.CreateUser(t.Context(), &owner, CreateUserRequest{Username: "target", DisplayName: "Target", Role: model.AdminRoleAdmin, Password: "target password long"}); err == nil {
		t.Fatal("random failure ignored")
	}
	if err := service.ChangePassword(t.Context(), &owner, pendingTestPassword, "replacement password long", ""); err == nil {
		t.Fatal("random failure ignored")
	}
	current, err := service.config.Store.GetUserByID(t.Context(), owner.User.ID)
	if err != nil || current.PasswordHash != owner.User.PasswordHash {
		t.Fatal("hash failure mutated account")
	}
}
