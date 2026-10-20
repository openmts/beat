package store

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type oidcAdmissionFixture struct {
	db        *sql.DB
	store     *AdminStore
	admission OIDCSessionAdmission
}

func newOIDCAdmissionFixture(t *testing.T) oidcAdmissionFixture {
	t.Helper()
	database, err := NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	target := NewAdminStore(database.DB)
	oidc := NewOIDCStore(database.DB)
	now := time.Now().UTC().Truncate(time.Microsecond)
	previous := now.Add(-time.Hour)
	user := model.AdminUser{ID: "oidc-user", Username: "oidc-user", DisplayName: "OIDC", Role: model.AdminRoleAdmin, PasswordHash: "hash", Enabled: true, PasswordChangedAt: previous, CreatedAt: previous, UpdatedAt: previous}
	provider := model.OIDCProvider{ID: "provider", Slug: "provider", DisplayName: "Provider", IssuerURL: "https://oidc.invalid", ClientID: "client", Enabled: true, ConfigurationVersion: 1, Scopes: []string{"openid"}, CreatedAt: previous, UpdatedAt: previous}
	identity := model.ExternalIdentity{ID: "identity", ProviderID: provider.ID, UserID: user.ID, Subject: "subject", LastLoginAt: previous, CreatedAt: previous, UpdatedAt: previous}
	session := model.AdminSession{ID: "session", UserID: user.ID, TokenHash: bytes.Repeat([]byte{1}, 32), TokenPrefix: "prefix", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour), AuthenticationMethod: model.AuthMethodOIDC, ExternalIdentityID: &identity.ID}
	audit := model.AdminAuditEvent{ID: "audit", RequestID: "request", ActorID: user.ID, ActorUsername: user.Username, Action: "auth.oidc.login", ResourceType: "provider", ResourceID: provider.ID, Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", SessionPrefix: session.TokenPrefix, CreatedAt: now}
	if err := target.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	if err := oidc.CreateProvider(t.Context(), &provider); err != nil {
		t.Fatal(err)
	}
	if err := oidc.LinkIdentity(t.Context(), &identity); err != nil {
		t.Fatal(err)
	}
	return oidcAdmissionFixture{db: database.DB, store: target, admission: OIDCSessionAdmission{Provider: provider, Identity: identity, User: user, Session: session, Audit: audit}}
}

func (f oidcAdmissionFixture) exec(t *testing.T, query string) {
	t.Helper()
	if _, err := f.db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func assertOIDCNotAdmitted(t *testing.T, f oidcAdmissionFixture) {
	t.Helper()
	for _, table := range []string{"admin_sessions", "admin_audit_events"} {
		var count int
		if err := f.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", table, count, err)
		}
	}
	var login sql.NullTime
	if err := f.db.QueryRow("SELECT last_login_at FROM admin_users WHERE id='oidc-user'").Scan(&login); err != nil || login.Valid {
		t.Fatal("partial user login", login, err)
	}
}

func TestOIDCAdmissionCommitsSessionTimesAndAudit(t *testing.T) {
	f := newOIDCAdmissionFixture(t)
	if err := f.store.AdmitOIDCSession(t.Context(), f.admission); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkOIDCAdmissionResult(t.Context(), tx, f.admission); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCAdmissionRejectsStaleAuthority(t *testing.T) {
	changes := map[string]string{
		"user_disabled":         "UPDATE admin_users SET enabled=0",
		"role_changed":          "UPDATE admin_users SET role='owner'",
		"provider_disabled":     "UPDATE admin_oidc_providers SET enabled=0",
		"configuration_changed": "UPDATE admin_oidc_providers SET configuration_version=2",
		"client_changed":        "UPDATE admin_oidc_providers SET client_id='other'",
		"issuer_changed":        "UPDATE admin_oidc_providers SET issuer_url='https://other.invalid'",
		"scopes_changed":        "UPDATE admin_oidc_providers SET scopes_json='[\"openid\",\"email\"]'",
		"unlinked":              "DELETE FROM admin_external_identities",
		"subject_changed":       "UPDATE admin_external_identities SET subject='other'",
		"relinked":              "UPDATE admin_external_identities SET id='replacement'",
	}
	for name, query := range changes {
		t.Run(name, func(t *testing.T) {
			f := newOIDCAdmissionFixture(t)
			f.exec(t, query)
			if err := f.store.AdmitOIDCSession(t.Context(), f.admission); err == nil {
				t.Fatal("stale identity admitted")
			}
			assertOIDCNotAdmitted(t, f)
		})
	}
}

func TestOIDCAdmissionRollsBackEveryMutationFailure(t *testing.T) {
	triggers := map[string]string{
		"session_abort":   "BEFORE INSERT ON admin_sessions BEGIN SELECT RAISE(ABORT,'failed'); END",
		"session_ignore":  "BEFORE INSERT ON admin_sessions BEGIN SELECT RAISE(IGNORE); END",
		"user_abort":      "BEFORE UPDATE OF last_login_at ON admin_users BEGIN SELECT RAISE(ABORT,'failed'); END",
		"user_ignore":     "BEFORE UPDATE OF last_login_at ON admin_users BEGIN SELECT RAISE(IGNORE); END",
		"identity_abort":  "BEFORE UPDATE OF last_login_at ON admin_external_identities BEGIN SELECT RAISE(ABORT,'failed'); END",
		"identity_ignore": "BEFORE UPDATE OF last_login_at ON admin_external_identities BEGIN SELECT RAISE(IGNORE); END",
		"audit_abort":     "BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'failed'); END",
		"audit_ignore":    "BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END",
		"audit_deleted":   "AFTER INSERT ON admin_audit_events BEGIN DELETE FROM admin_audit_events WHERE id=NEW.id; END",
		"late_disable":    "AFTER INSERT ON admin_audit_events BEGIN UPDATE admin_oidc_providers SET enabled=0; END",
		"late_unlink":     "AFTER INSERT ON admin_audit_events BEGIN DELETE FROM admin_external_identities; END",
		"session_changed": "AFTER INSERT ON admin_sessions BEGIN UPDATE admin_sessions SET authentication_method='local',external_identity_id=NULL; END",
		"login_changed":   "AFTER UPDATE OF last_login_at ON admin_users BEGIN UPDATE admin_users SET last_login_at=NULL; END",
	}
	for name, body := range triggers {
		t.Run(name, func(t *testing.T) {
			f := newOIDCAdmissionFixture(t)
			f.exec(t, "CREATE TRIGGER fail_admission "+body)
			if err := f.store.AdmitOIDCSession(t.Context(), f.admission); err == nil {
				t.Fatal("partial admission committed")
			}
			assertOIDCNotAdmitted(t, f)
			var login time.Time
			if err := f.db.QueryRow("SELECT last_login_at FROM admin_external_identities").Scan(&login); err != nil || !login.Equal(f.admission.Identity.LastLoginAt) {
				t.Fatal("partial identity login", login, err)
			}
		})
	}
}
