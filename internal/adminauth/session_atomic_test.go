package adminauth

import (
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestSessionSecurityAuditFailureRollsBackMutation(t *testing.T) {
	for _, action := range []string{"logout", "reauthenticate"} {
		t.Run(action, func(t *testing.T) {
			service, database, principal := atomicSessionFixture(t)
			guarded, stop, err := service.GuardSession(t.Context(), principal)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			if _, err := database.DB.Exec(`CREATE TRIGGER reject_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`); err != nil {
				t.Fatal(err)
			}
			if action == "logout" {
				err = service.Logout(t.Context(), &principal)
			} else {
				err = service.Reauthenticate(t.Context(), &principal, pendingTestPassword, "")
			}
			if err == nil {
				t.Fatal("audit failure accepted")
			}
			session, err := service.config.Store.GetSessionByID(t.Context(), principal.Session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if session.RevokedAt != nil || session.ReauthenticatedUntil != nil || principal.Session.ReauthenticatedUntil != nil {
				t.Fatal("failed audited mutation changed session")
			}
			if guarded.Err() != nil {
				t.Fatal("failed logout closed live session")
			}
			if _, err := database.DB.Exec("DROP TRIGGER reject_audit"); err != nil {
				t.Fatal(err)
			}
			if action == "logout" {
				err = service.Logout(t.Context(), &principal)
			} else {
				err = service.Reauthenticate(t.Context(), &principal, pendingTestPassword, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			if action == "logout" && guarded.Err() == nil {
				t.Fatal("successful logout did not close live session")
			}
			assertSingleSessionAudit(t, service, sessionAuditExpectation{principal, action})
		})
	}
}

func atomicSessionFixture(t *testing.T) (*Service, *store.SQLiteStore, model.AdminPrincipal) {
	t.Helper()
	database, secrets := authDependencies(t)
	service, err := NewService(ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: secrets, LegacyToken: "bootstrap-secret", Passwords: NewPasswordHasher(PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	return service, database, pendingTestOwner(t, service)
}

type sessionAuditExpectation struct {
	principal model.AdminPrincipal
	action    string
}

func assertSingleSessionAudit(t *testing.T, service *Service, want sessionAuditExpectation) {
	t.Helper()
	page, err := service.ListAuditEvents(t.Context(), &want.principal, model.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range page.Events {
		if event.Action == "auth."+want.action {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("success audit count %d", count)
	}
}
