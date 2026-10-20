package adminauth

import (
	"bytes"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func selfSecurityService(t *testing.T) (*Service, *sql.DB, model.AdminPrincipal) {
	t.Helper()
	sqlite, secrets := authDependencies(t)
	service, err := NewService(ServiceConfig{Store: store.NewAdminStore(sqlite.DB), Secrets: secrets, LegacyToken: "bootstrap-secret", Passwords: NewPasswordHasher(PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	owner := pendingTestOwner(t, service)
	if err := service.Reauthenticate(t.Context(), &owner, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	return service, sqlite.DB, owner
}

func selfAuditFault(t *testing.T, database *sql.DB, enable bool) {
	t.Helper()
	statement := "DROP TRIGGER fail_self_audit"
	if enable {
		statement = "CREATE TRIGGER fail_self_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"
	}
	if _, err := database.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func TestSelfSecurityTOTPAuditFailurePreservesPendingAndFactor(t *testing.T) {
	service, database, owner := selfSecurityService(t)
	setup, err := service.BeginTOTP(t.Context(), &owner)
	if err != nil {
		t.Fatal(err)
	}
	old := service.pendingTOTP.entries[owner.Session.ID]
	selfAuditFault(t, database, true)
	if _, err := service.BeginTOTP(t.Context(), &owner); !errors.Is(err, store.ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if !bytes.Equal(service.pendingTOTP.entries[owner.Session.ID].secret, old.secret) {
		t.Fatal("failed begin replaced pending setup")
	}
	code := pendingTestCode(t, service, setup.Secret)
	if err := service.EnableTOTP(t.Context(), &owner, code); !errors.Is(err, store.ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if !bytes.Equal(service.pendingTOTP.entries[owner.Session.ID].secret, old.secret) {
		t.Fatal("failed enable cleared pending setup")
	}
	user, err := service.config.Store.GetUserByID(t.Context(), owner.User.ID)
	if err != nil || user.TOTPEnabled() {
		t.Fatal("failed enable changed factor")
	}
	selfAuditFault(t, database, false)
	if err := service.EnableTOTP(t.Context(), &owner, code); err != nil {
		t.Fatal(err)
	}
	assertSelfDisableRollback(t, database, selfDisableTarget{service, owner})
}

type selfDisableTarget struct {
	service *Service
	owner   model.AdminPrincipal
}

func assertSelfDisableRollback(t *testing.T, database *sql.DB, target selfDisableTarget) {
	t.Helper()
	service, owner := target.service, target.owner
	replacement, err := service.BeginTOTP(t.Context(), &owner)
	if err != nil {
		t.Fatal(err)
	}
	selfAuditFault(t, database, true)
	if err := service.DisableTOTP(t.Context(), &owner); !errors.Is(err, store.ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	user, err := service.config.Store.GetUserByID(t.Context(), owner.User.ID)
	if err != nil || !user.TOTPEnabled() {
		t.Fatal("failed disable changed factor")
	}
	if string(service.pendingTOTP.entries[owner.Session.ID].secret) != replacement.Secret {
		t.Fatal("failed disable cleared pending setup")
	}
	selfAuditFault(t, database, false)
	if err := service.DisableTOTP(t.Context(), &owner); err != nil {
		t.Fatal(err)
	}
	if len(service.pendingTOTP.entries) != 0 {
		t.Fatal("successful disable retained pending setup")
	}
}

func TestSelfSecurityRevokeFailureKeepsLiveAndPending(t *testing.T) {
	for _, others := range []bool{false, true} {
		t.Run(map[bool]string{false: "one", true: "others"}[others], func(t *testing.T) {
			service, database, owner := selfSecurityService(t)
			login, err := service.Login(t.Context(), LoginRequest{Username: owner.User.Username, Password: pendingTestPassword})
			if err != nil {
				t.Fatal(err)
			}
			target := login.Principal
			if err := service.Reauthenticate(t.Context(), &target, pendingTestPassword, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := service.BeginTOTP(t.Context(), &target); err != nil {
				t.Fatal(err)
			}
			live := liveTestGuard(t, service, target)
			selfAuditFault(t, database, true)
			revoke := func() error {
				if others {
					_, err := service.RevokeOtherSessions(t.Context(), &owner)
					return err
				}
				return service.RevokeSession(t.Context(), &owner, target.Session.ID)
			}
			if err := revoke(); !errors.Is(err, store.ErrAdminMutationUnavailable) {
				t.Fatal(err)
			}
			if live.Err() != nil {
				t.Fatal("audit failure closed live session")
			}
			if _, ok := service.pendingTOTP.entries[target.Session.ID]; !ok {
				t.Fatal("audit failure cleared pending setup")
			}
			selfAuditFault(t, database, false)
			if err := revoke(); err != nil {
				t.Fatal(err)
			}
			assertLiveCanceled(t, live)
			if _, ok := service.pendingTOTP.entries[target.Session.ID]; ok {
				t.Fatal("committed revoke retained pending setup")
			}
		})
	}
}

func TestSelfSecurityNilPrincipal(t *testing.T) {
	service := newTestService(t)
	if err := service.RevokeSession(t.Context(), nil, "target"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	if _, err := service.RevokeOtherSessions(t.Context(), nil); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	if err := service.DisableTOTP(t.Context(), nil); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
}

func TestSelfSecurityRechecksStaleRoleAndExpiredProof(t *testing.T) {
	service, _, owner := selfSecurityService(t)
	setup, err := service.BeginTOTP(t.Context(), &owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.config.Store.MarkSessionReauthenticated(t.Context(), owner.Session.ID, service.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	change := service.selfSecurityChange(owner, store.AdministratorTOTPBegin, owner.User.ID)
	if _, err := service.commitSelfSecurity(t.Context(), change); !errors.Is(err, ErrRecentAuthRequired) {
		t.Fatal(err)
	}
	if string(service.pendingTOTP.entries[owner.Session.ID].secret) != setup.Secret {
		t.Fatal("expired proof cleared setup")
	}
	if err := service.config.Store.RevokeSession(t.Context(), owner.Session.ID, service.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevokeOtherSessions(t.Context(), &owner); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
}

func TestSelfSecurityRejectsRoleMismatchBeforeSecretChange(t *testing.T) {
	service, _, owner := selfSecurityService(t)
	owner.User.Role = model.AdminRoleAdmin
	if _, err := service.BeginTOTP(t.Context(), &owner); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
	if err := service.DisableTOTP(t.Context(), &owner); !errors.Is(err, ErrSessionInvalid) {
		t.Fatal(err)
	}
}
