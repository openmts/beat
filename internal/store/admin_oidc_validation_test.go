package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestOIDCAdmissionValidatesAttributionAndAudit(t *testing.T) {
	f := newOIDCAdmissionFixture(t)
	cases := map[string]func(*OIDCSessionAdmission){
		"provider": func(a *OIDCSessionAdmission) { a.Provider.Enabled = false },
		"identity": func(a *OIDCSessionAdmission) { a.Identity.UserID = "other" },
		"method":   func(a *OIDCSessionAdmission) { a.Session.AuthenticationMethod = model.AuthMethodLocal },
		"external": func(a *OIDCSessionAdmission) { a.Session.ExternalIdentityID = nil },
		"token":    func(a *OIDCSessionAdmission) { a.Session.TokenHash = nil },
		"audit":    func(a *OIDCSessionAdmission) { a.Audit.Action = "auth.login" },
		"request":  func(a *OIDCSessionAdmission) { a.Audit.RequestID = "" },
		"resource": func(a *OIDCSessionAdmission) { a.Audit.ResourceID = "other" },
		"actor":    func(a *OIDCSessionAdmission) { a.Audit.ActorID = "other" },
		"expired":  func(a *OIDCSessionAdmission) { a.Audit.CreatedAt = a.Session.AbsoluteExpiresAt },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			admission := f.admission
			change(&admission)
			if err := f.store.AdmitOIDCSession(t.Context(), admission); err == nil {
				t.Fatal("invalid admission accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.store.AdmitOIDCSession(ctx, f.admission); err == nil {
		t.Fatal("canceled admission accepted")
	}
	assertOIDCNotAdmitted(t, f)
}

func TestOIDCAdmissionFailsClosedOnMissingSnapshotTables(t *testing.T) {
	for _, table := range []string{"admin_oidc_providers", "admin_external_identities"} {
		t.Run(table, func(t *testing.T) {
			f := newOIDCAdmissionFixture(t)
			f.exec(t, "ALTER TABLE "+table+" RENAME TO archived_table")
			if err := f.store.AdmitOIDCSession(t.Context(), f.admission); err == nil {
				t.Fatal("missing authority table admitted")
			}
			assertOIDCNotAdmitted(t, f)
		})
	}
}

func TestOIDCAdmissionCommitFailureRollsBackEverything(t *testing.T) {
	f := newOIDCAdmissionFixture(t)
	f.exec(t, `CREATE TABLE oidc_commit_failure(user_id TEXT REFERENCES admin_users(id) DEFERRABLE INITIALLY DEFERRED)`)
	f.exec(t, `CREATE TRIGGER fail_oidc_commit AFTER INSERT ON admin_audit_events BEGIN INSERT INTO oidc_commit_failure VALUES('missing'); END`)
	if err := f.store.AdmitOIDCSession(t.Context(), f.admission); err == nil {
		t.Fatal("deferred constraint failure committed")
	}
	assertOIDCNotAdmitted(t, f)
}
