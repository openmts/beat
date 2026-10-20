package adminauth

import (
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestLoginAuditFailureDoesNotCreateSessionOrTouchUser(t *testing.T) {
	service, database, principal := atomicSessionFixture(t)
	before, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(t.Context(), LoginRequest{Username: principal.User.Username, Password: pendingTestPassword})
	if err == nil || result.Token != "" {
		t.Fatal("login succeeded without audit")
	}
	sessions, err := service.config.Store.ListSessions(t.Context(), principal.User.ID, model.NowUTC(), model.AdminSessionPageSize)
	if err != nil || len(sessions) != 1 {
		t.Fatal("failed login persisted a session", err)
	}
	after, err := service.config.Store.GetUserByID(t.Context(), principal.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.LastLoginAt == nil && after.LastLoginAt != nil || before.LastLoginAt != nil && (after.LastLoginAt == nil || !before.LastLoginAt.Equal(*after.LastLoginAt)) {
		t.Fatal("failed login advanced last login")
	}
}

func TestBootstrapAuditFailureLeavesSetupAvailable(t *testing.T) {
	database, secrets := authDependencies(t)
	service, err := NewService(ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: secrets, LegacyToken: "bootstrap-secret", Passwords: NewPasswordHasher(PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	request := BootstrapRequest{BootstrapToken: "bootstrap-secret", Username: "owner", DisplayName: "Owner", Password: pendingTestPassword}
	result, err := service.Bootstrap(t.Context(), request)
	if err == nil || result.Token != "" {
		t.Fatal("bootstrap succeeded without audit")
	}
	state, err := service.State(t.Context())
	if err != nil || !state.SetupRequired {
		t.Fatal("failed bootstrap consumed setup", err)
	}
	if _, err := database.DB.Exec("DROP TRIGGER fail_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(t.Context(), request); err != nil {
		t.Fatal("bootstrap retry failed", err)
	}
}

type loginMutationPasswords struct {
	passwordOperations
	after func()
}

func (p loginMutationPasswords) Verify(password, hash string) (bool, error) {
	valid, err := p.passwordOperations.Verify(password, hash)
	p.after()
	return valid, err
}

func TestLoginRejectsSecurityChangeDuringPasswordVerification(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"disabled", `UPDATE admin_users SET enabled=0`},
		{"password", `UPDATE admin_users SET password_hash='replacement'`},
		{"role", `UPDATE admin_users SET role='admin'`},
		{"factor", `UPDATE admin_users SET totp_secret_encrypted=x'01',totp_enabled_at=CURRENT_TIMESTAMP`},
	} {
		t.Run(change.name, func(t *testing.T) {
			service, database, principal := atomicSessionFixture(t)
			service.config.Passwords = loginMutationPasswords{service.config.Passwords, func() {
				if _, err := database.DB.Exec(change.sql); err != nil {
					t.Fatal(err)
				}
			}}
			result, err := service.Login(t.Context(), LoginRequest{Username: principal.User.Username, Password: pendingTestPassword})
			if err == nil || result.Token != "" {
				t.Fatal("stale authentication granted a session")
			}
			sessions, err := service.config.Store.ListSessions(t.Context(), principal.User.ID, model.NowUTC(), model.AdminSessionPageSize)
			if err != nil || len(sessions) != 1 {
				t.Fatal("stale authentication persisted a session", err)
			}
		})
	}
}
