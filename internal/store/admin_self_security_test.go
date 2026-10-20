package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/beat/backend/internal/model"
)

func selfSecurityFixture(t *testing.T) (administratorFixture, SelfSecurityChange) {
	t.Helper()
	f := newAdministratorFixture(t)
	second := administratorSession(f.principal.User.ID, "other", f.now)
	if err := f.store.CreateSession(t.Context(), &second); err != nil {
		t.Fatal(err)
	}
	audit := f.change(AdministratorTOTPBegin, f.principal.User).Audit
	audit.ActorUsername = f.principal.User.Username
	audit.SessionPrefix = f.principal.Session.TokenPrefix
	return f, SelfSecurityChange{Principal: f.principal, Audit: audit, Secret: []byte("new encrypted factor"), PreviousSecret: f.principal.User.TOTPSecretEncrypted}
}

func setSelfSecurityAction(change SelfSecurityChange, action string) SelfSecurityChange {
	change.Audit.Action, change.Audit.ID = action, action
	if action == AdministratorSessionRevoke {
		change.Audit.ResourceType, change.Audit.ResourceID = "session", "other"
	}
	return change
}

func TestSelfSecurityAtomicSuccessAndAuditRollback(t *testing.T) {
	for _, action := range []string{AdministratorSessionRevoke, AdministratorSessionsRevoke, AdministratorTOTPBegin, AdministratorTOTPEnable, AdministratorTOTPDisable} {
		t.Run(action, func(t *testing.T) {
			f, change := selfSecurityFixture(t)
			change = setSelfSecurityAction(change, action)
			administratorTrigger(t, f, "CREATE TRIGGER fail_self_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END")
			if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
				t.Fatalf("audit failure: %v", err)
			}
			user, err := f.store.GetUserByID(t.Context(), f.principal.User.ID)
			if err != nil || string(user.TOTPSecretEncrypted) != string(f.principal.User.TOTPSecretEncrypted) || user.TOTPEnabledAt != nil {
				t.Fatalf("factor escaped rollback: %+v %v", user, err)
			}
			session, err := f.store.GetSessionByID(t.Context(), "other")
			if err != nil || session.RevokedAt != nil {
				t.Fatal("revocation escaped rollback")
			}
			administratorTrigger(t, f, "DROP TRIGGER fail_self_audit")
			count, err := f.store.CommitSelfSecurity(t.Context(), change)
			if err != nil {
				t.Fatal(err)
			}
			if action == AdministratorSessionRevoke || action == AdministratorSessionsRevoke {
				if count != 1 {
					t.Fatalf("revoked=%d", count)
				}
			}
			if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 1 {
				t.Fatal("success audit missing")
			}
			if fmt.Sprintf("%#v", change) != "SelfSecurityChange{redacted}" {
				t.Fatal("secret logging not redacted")
			}
		})
	}
}

func TestSelfSecurityOwnScopeAndCurrentSession(t *testing.T) {
	f, change := selfSecurityFixture(t)
	otherUser := testAdminUser("foreign", model.AdminRoleAdmin)
	if err := f.store.CreateUser(t.Context(), &otherUser); err != nil {
		t.Fatal(err)
	}
	foreign := administratorSession(otherUser.ID, "foreign-session", f.now)
	if err := f.store.CreateSession(t.Context(), &foreign); err != nil {
		t.Fatal(err)
	}
	change = setSelfSecurityAction(change, AdministratorSessionRevoke)
	change.Audit.ResourceID = foreign.ID
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminSessionNotFound) {
		t.Fatal(err)
	}
	change.Audit.ResourceID = "missing"
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminSessionNotFound) {
		t.Fatal(err)
	}
	change.Audit.ResourceID = f.principal.Session.ID
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal("revoked actor retained authority")
	}
}

func TestSelfSecurityTOTPCompareAndSwapAndIgnoredWrites(t *testing.T) {
	f, change := selfSecurityFixture(t)
	change = setSelfSecurityAction(change, AdministratorTOTPEnable)
	change.PreviousSecret = []byte("stale")
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationConflict) {
		t.Fatal(err)
	}
	change.PreviousSecret = f.principal.User.TOTPSecretEncrypted
	administratorTrigger(t, f, "CREATE TRIGGER ignore_totp BEFORE UPDATE ON admin_users BEGIN SELECT RAISE(IGNORE); END")
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	administratorTrigger(t, f, "DROP TRIGGER ignore_totp")
	administratorTrigger(t, f, "CREATE TRIGGER corrupt_totp AFTER UPDATE ON admin_users BEGIN UPDATE admin_users SET totp_secret_encrypted=x'00' WHERE id=NEW.id; END")
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	administratorTrigger(t, f, "DROP TRIGGER corrupt_totp")
	administratorTrigger(t, f, "CREATE TRIGGER ignore_revoke BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(IGNORE); END")
	change = setSelfSecurityAction(change, AdministratorSessionsRevoke)
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
}

func TestSelfSecurityAuthorityAndEnvelope(t *testing.T) {
	for _, action := range []string{AdministratorSessionRevoke, AdministratorTOTPBegin} {
		t.Run(action, func(t *testing.T) {
			f, change := selfSecurityFixture(t)
			change = setSelfSecurityAction(change, action)
			administratorTrigger(t, f, "UPDATE admin_sessions SET reauthenticated_until=NULL")
			_, err := f.store.CommitSelfSecurity(t.Context(), change)
			if action == AdministratorSessionRevoke {
				if err != nil {
					t.Fatal("ordinary own-session revoke requires recent proof")
				}
			} else if !errors.Is(err, ErrAdminMutationRecent) {
				t.Fatal(err)
			}
		})
	}
	f, change := selfSecurityFixture(t)
	for _, mutate := range []func(*SelfSecurityChange){func(c *SelfSecurityChange) { c.Audit.ID = "" }, func(c *SelfSecurityChange) { c.Audit.Action = "unknown" }, func(c *SelfSecurityChange) { c.Audit.ResourceID = "foreign" }, func(c *SelfSecurityChange) { c.Audit.Action = AdministratorTOTPEnable; c.Secret = nil }} {
		invalid := change
		mutate(&invalid)
		if _, err := f.store.CommitSelfSecurity(t.Context(), invalid); !errors.Is(err, ErrAdminMutationDenied) {
			t.Fatal(err)
		}
	}
	administratorTrigger(t, f, "UPDATE admin_users SET role='admin'")
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal("stale role accepted")
	}
}
