package store

import (
	"errors"
	"testing"
)

func TestSelfSecurityCommitAndDatabaseFaults(t *testing.T) {
	for _, kind := range []string{"commit", "session write", "factor write"} {
		t.Run(kind, func(t *testing.T) {
			f, change := selfSecurityFixture(t)
			switch kind {
			case "commit":
				administratorTrigger(t, f, "CREATE TABLE self_commit_guard(parent TEXT REFERENCES admin_users(id) DEFERRABLE INITIALLY DEFERRED)")
				administratorTrigger(t, f, "CREATE TRIGGER fail_self_commit AFTER INSERT ON admin_audit_events BEGIN INSERT INTO self_commit_guard VALUES('missing'); END")
			case "session write":
				change = setSelfSecurityAction(change, AdministratorSessionsRevoke)
				administratorTrigger(t, f, "CREATE TRIGGER fail_self_session BEFORE UPDATE ON admin_sessions BEGIN SELECT RAISE(ABORT,'session unavailable'); END")
			case "factor write":
				change = setSelfSecurityAction(change, AdministratorTOTPEnable)
				administratorTrigger(t, f, "CREATE TRIGGER fail_self_factor BEFORE UPDATE ON admin_users BEGIN SELECT RAISE(ABORT,'factor unavailable'); END")
			}
			if _, err := f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
				t.Fatal(err)
			}
			if administratorCount(t, f, "SELECT count(*) FROM admin_audit_events") != 0 {
				t.Fatal("failure committed success audit")
			}
		})
	}
}

func TestSelfSecurityClosedTransactionAndMissingFactorTargets(t *testing.T) {
	f, change := selfSecurityFixture(t)
	change = setSelfSecurityAction(change, AdministratorTOTPEnable)
	tx, err := f.store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = authorizeSelfSecurity(t.Context(), tx, change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if err = writeSelfTOTP(t.Context(), tx, change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if _, err = revokeSelfSessions(t.Context(), tx, change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
	if selfTOTPResult(nil, change) {
		t.Fatal("missing factor accepted")
	}
	change.Principal.User.PasswordHash = "stale-password"
	if _, err = f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationDenied) {
		t.Fatal("stale password snapshot accepted")
	}
	if err = f.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.CommitSelfSecurity(t.Context(), change); !errors.Is(err, ErrAdminMutationUnavailable) {
		t.Fatal(err)
	}
}

func TestSelfSecurityOrdinaryAdminAndZeroOtherSessions(t *testing.T) {
	f, change := selfSecurityFixture(t)
	administratorTrigger(t, f, "UPDATE admin_users SET role='admin'")
	change.Principal.User.Role = "admin"
	change = setSelfSecurityAction(change, AdministratorSessionsRevoke)
	if _, err := f.store.CommitSelfSecurity(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	change.Audit.ID = "no-others"
	count, err := f.store.CommitSelfSecurity(t.Context(), change)
	if err != nil || count != 0 {
		t.Fatalf("zero others: %d %v", count, err)
	}
}
