package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func sessionSecurityFixture(t *testing.T) (*AdminStore, SessionSecurityChange) {
	t.Helper()
	sqlite := setupTestDB(t)
	database := NewAdminStore(sqlite.DB)
	user := testAdminUser("security-owner", model.AdminRoleOwner)
	if err := database.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	expiry := now.Add(time.Hour)
	session := model.AdminSession{ID: "security-session", UserID: user.ID, TokenHash: []byte("hash"), TokenPrefix: "prefix", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: expiry, AbsoluteExpiresAt: expiry}
	if err := database.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	until := now.Add(10 * time.Minute)
	change := SessionSecurityChange{Principal: model.AdminPrincipal{User: user, Session: session}, ReauthenticatedUntil: &until,
		Audit: model.AdminAuditEvent{ID: "audit", RequestID: "request", ActorID: user.ID, ActorUsername: user.Username, SessionPrefix: "prefix", Action: "auth.reauthenticate", ResourceType: "session", ResourceID: session.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", CreatedAt: now}}
	return database, change
}

func TestSessionSecurityRejectsStaleAuthorityAndProof(t *testing.T) {
	cases := []struct{ name, sql string }{
		{"password", "UPDATE admin_users SET password_hash='replacement'"},
		{"factor", "UPDATE admin_users SET totp_secret_encrypted=x'01'"},
		{"factor-enabled", "UPDATE admin_users SET totp_enabled_at=CURRENT_TIMESTAMP"},
		{"role", "UPDATE admin_users SET role='admin'"},
		{"disabled", "UPDATE admin_users SET enabled=0"},
		{"revoked", "UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP"},
		{"expired", "UPDATE admin_sessions SET idle_expires_at='2000-01-01 00:00:00'"},
		{"deleted", "DELETE FROM admin_sessions"},
		{"username", "UPDATE admin_users SET username='changed'"},
		{"prefix", "UPDATE admin_sessions SET token_prefix='changed'"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			database, change := sessionSecurityFixture(t)
			if _, err := database.db.Exec(test.sql); err != nil {
				t.Fatal(err)
			}
			if err := database.CommitSessionSecurity(t.Context(), change); err == nil {
				t.Fatal("stale security state accepted")
			}
			var count int
			if err := database.db.QueryRow("SELECT count(*) FROM admin_audit_events").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("rejected mutation recorded success")
			}
		})
	}
}

func TestSessionSecurityRejectsFalsePersistenceAndAuditFailure(t *testing.T) {
	for _, action := range []string{"auth.logout", "auth.reauthenticate"} {
		for _, fault := range []string{
			`CREATE TRIGGER fault BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(IGNORE); END`,
			`CREATE TRIGGER fault BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(ABORT, 'injected'); END`,
			`CREATE TRIGGER fault AFTER UPDATE ON admin_sessions BEGIN UPDATE admin_sessions SET revoked_at=NULL,reauthenticated_until=NULL WHERE id=NEW.id; END`,
			`CREATE TRIGGER fault AFTER UPDATE ON admin_sessions BEGIN DELETE FROM admin_sessions WHERE id=NEW.id; END`,
			`CREATE TRIGGER fault BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END`,
		} {
			t.Run(action+fault[:27], func(t *testing.T) {
				database, change := sessionSecurityFixture(t)
				change.Audit.Action = action
				if action == "auth.logout" {
					change.ReauthenticatedUntil = nil
				}
				if _, err := database.db.Exec(fault); err != nil {
					t.Fatal(err)
				}
				if err := database.CommitSessionSecurity(t.Context(), change); err == nil {
					t.Fatal("failed persistence accepted")
				}
				session, err := database.GetSessionByID(t.Context(), change.Principal.Session.ID)
				if err != nil || session == nil || session.RevokedAt != nil || session.ReauthenticatedUntil != nil {
					t.Fatal("failed transaction changed session", err)
				}
			})
		}
	}
}

func TestSessionSecurityValidatesMutationEnvelope(t *testing.T) {
	for _, mutate := range []func(*SessionSecurityChange){
		func(c *SessionSecurityChange) { c.Audit.ID = "" },
		func(c *SessionSecurityChange) { c.Audit.ResourceID = "another-session" },
		func(c *SessionSecurityChange) { c.Audit.ActorID = "another-user" },
		func(c *SessionSecurityChange) { c.Audit.Outcome = model.AuditOutcomeFailure },
		func(c *SessionSecurityChange) { c.Audit.Action = "invalid" },
		func(c *SessionSecurityChange) { c.ReauthenticatedUntil = &c.Audit.CreatedAt },
		func(c *SessionSecurityChange) { c.ReauthenticatedUntil = nil },
	} {
		database, change := sessionSecurityFixture(t)
		mutate(&change)
		if err := database.CommitSessionSecurity(t.Context(), change); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
}

func TestSessionSecuritySuccessAndUnavailableDatabase(t *testing.T) {
	database, change := sessionSecurityFixture(t)
	if err := database.CommitSessionSecurity(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	session, err := database.GetSessionByID(t.Context(), change.Principal.Session.ID)
	if err != nil || !sameOptionalTime(session.ReauthenticatedUntil, change.ReauthenticatedUntil) {
		t.Fatal("proof missing", err)
	}
	change.Audit.Action = "auth.logout"
	change.Audit.ID = "logout"
	change.ReauthenticatedUntil = nil
	if err := database.CommitSessionSecurity(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	session, err = database.GetSessionByID(t.Context(), change.Principal.Session.ID)
	if err != nil || session.RevokedAt == nil || session.ReauthenticatedUntil != nil {
		t.Fatal("logout did not clear proof", err)
	}
	if err := database.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitSessionSecurity(t.Context(), change); err == nil {
		t.Fatal("closed database accepted")
	}
}

func TestSessionSecurityReadFailuresNeverGrantProof(t *testing.T) {
	for _, statement := range []string{
		`UPDATE admin_sessions SET created_at='invalid-time'`,
		`ALTER TABLE admin_users RENAME TO unavailable_users`,
		`CREATE TRIGGER corrupt_result AFTER UPDATE ON admin_sessions BEGIN UPDATE admin_sessions SET created_at='invalid-time' WHERE id=NEW.id; END`,
	} {
		database, change := sessionSecurityFixture(t)
		if _, err := database.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		if err := database.CommitSessionSecurity(t.Context(), change); err == nil {
			t.Fatal("unreadable security state accepted")
		}
		var count int
		if err := database.db.QueryRow("SELECT count(*) FROM admin_audit_events").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("read failure recorded success")
		}
	}
}
