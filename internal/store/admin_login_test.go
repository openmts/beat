package store

import (
	"bytes"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func localAdmissionFixture(t *testing.T, bootstrap bool) (*AdminStore, LocalSessionAdmission) {
	t.Helper()
	database, change := sessionSecurityFixture(t)
	a := LocalSessionAdmission{User: change.Principal.User, Session: change.Principal.Session, Audit: change.Audit, Bootstrap: bootstrap}
	a.Session.ID = "new-session"
	a.Session.TokenHash = []byte("new-hash")
	a.Session.AuthenticationMethod = model.AuthMethodLocal
	a.Audit.Action, a.Audit.ResourceID = "auth.login", a.Session.ID
	if bootstrap {
		if _, err := database.db.Exec("DELETE FROM admin_sessions"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.db.Exec("DELETE FROM admin_users"); err != nil {
			t.Fatal(err)
		}
		a.Audit.Action, a.Audit.ResourceType, a.Audit.ResourceID = "auth.bootstrap", "administrator", a.User.ID
	}
	return database, a
}

func TestLocalAdmissionAtomicFaults(t *testing.T) {
	for _, bootstrap := range []bool{false, true} {
		for _, fault := range []string{
			`CREATE TRIGGER fault BEFORE INSERT ON admin_sessions BEGIN SELECT RAISE(IGNORE); END`,
			`CREATE TRIGGER fault AFTER INSERT ON admin_sessions BEGIN UPDATE admin_sessions SET token_hash=x'01' WHERE id=NEW.id; END`,
			`CREATE TRIGGER fault AFTER INSERT ON admin_sessions BEGIN DELETE FROM admin_sessions WHERE id=NEW.id; END`,
			`CREATE TRIGGER fault BEFORE UPDATE OF last_login_at ON admin_users BEGIN SELECT RAISE(ABORT,'injected'); END`,
			`CREATE TRIGGER fault AFTER UPDATE OF last_login_at ON admin_users BEGIN UPDATE admin_users SET last_login_at=NULL WHERE id=NEW.id; END`,
			`CREATE TRIGGER fault AFTER UPDATE OF last_login_at ON admin_users BEGIN UPDATE admin_users SET enabled=0 WHERE id=NEW.id; END`,
			`CREATE TRIGGER fault BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE); END`,
		} {
			database, a := localAdmissionFixture(t, bootstrap)
			if _, err := database.db.Exec(fault); err != nil {
				t.Fatal(err)
			}
			if err := database.AdmitLocalSession(t.Context(), a); err == nil {
				t.Fatal("injected failure admitted session")
			}
			assertLocalAdmissionAbsent(t, database, a)
		}
	}
}

func assertLocalAdmissionAbsent(t *testing.T, database *AdminStore, a LocalSessionAdmission) {
	t.Helper()
	session, err := database.GetSessionByID(t.Context(), a.Session.ID)
	if err != nil || session != nil {
		t.Fatal("failed admission persisted session", err)
	}
	var count int
	if err := database.db.QueryRow("SELECT count(*) FROM admin_audit_events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed admission recorded success")
	}
	user, err := database.GetUserByID(t.Context(), a.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Bootstrap && user != nil {
		t.Fatal("failed bootstrap persisted owner")
	}
	if !a.Bootstrap && (user == nil || !user.Enabled || user.LastLoginAt != nil) {
		t.Fatal("failed admission changed user")
	}
}

func TestLocalAdmissionValidatesEnvelope(t *testing.T) {
	for _, mutate := range []func(*LocalSessionAdmission){
		func(a *LocalSessionAdmission) { a.Session.UserID = "mismatch" },
		func(a *LocalSessionAdmission) { a.Audit.ID = "" },
		func(a *LocalSessionAdmission) { a.Audit.Action = "auth.logout" },
		func(a *LocalSessionAdmission) { a.Bootstrap = true },
		func(a *LocalSessionAdmission) { a.Session.ReauthenticatedUntil = &a.Session.CreatedAt },
		func(a *LocalSessionAdmission) { a.Session.AuthenticationMethod = model.AuthMethodOIDC },
	} {
		database, a := localAdmissionFixture(t, false)
		mutate(&a)
		if err := database.AdmitLocalSession(t.Context(), a); err == nil {
			t.Fatal("invalid admission accepted")
		}
	}
}

func TestLocalAdmissionSuccessAndSingleBootstrap(t *testing.T) {
	database, a := localAdmissionFixture(t, true)
	a.Session.AuthenticationMethod = ""
	if err := database.AdmitLocalSession(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	session, err := database.GetSessionByID(t.Context(), a.Session.ID)
	if err != nil || session == nil || session.AuthenticationMethod != model.AuthMethodLocal {
		t.Fatal("local attribution missing", err)
	}
	if err := database.AdmitLocalSession(t.Context(), a); err == nil {
		t.Fatal("second bootstrap admitted")
	}
	a.Bootstrap = false
	a.Session.ID = "second-session"
	a.Session.TokenHash = []byte("second-hash")
	a.Audit.ID, a.Audit.Action, a.Audit.ResourceType, a.Audit.ResourceID = "second-audit", "auth.login", "session", a.Session.ID
	if err := database.AdmitLocalSession(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := database.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.AdmitLocalSession(t.Context(), a); err == nil {
		t.Fatal("closed database admitted")
	}
}

func TestLocalAdmissionStorageAndSnapshotFailures(t *testing.T) {
	for _, test := range []struct {
		bootstrap bool
		statement string
	}{
		{false, `UPDATE admin_users SET password_hash='changed'`},
		{false, `UPDATE admin_users SET password_changed_at='invalid-time'`},
		{false, `CREATE TRIGGER corrupt_session AFTER INSERT ON admin_sessions BEGIN UPDATE admin_sessions SET created_at='invalid-time' WHERE id=NEW.id; END`},
		{true, `DROP TABLE admin_users`},
		{true, `CREATE TRIGGER reject_owner BEFORE INSERT ON admin_users BEGIN SELECT RAISE(IGNORE); END`},
	} {
		database, a := localAdmissionFixture(t, test.bootstrap)
		if _, err := database.db.Exec(test.statement); err != nil {
			t.Fatal(err)
		}
		if err := database.AdmitLocalSession(t.Context(), a); err == nil {
			t.Fatal("unusable admission evidence accepted")
		}
		session, err := database.GetSessionByID(t.Context(), a.Session.ID)
		if err != nil || session != nil {
			t.Fatal("failed admission persisted session", err)
		}
	}
}

func TestLocalAdmissionCommitFailureRollsBackAllWrites(t *testing.T) {
	database, a := localAdmissionFixture(t, true)
	if _, err := database.db.Exec(`CREATE TRIGGER invalid_actor AFTER INSERT ON admin_audit_events BEGIN UPDATE admin_sessions SET user_id='missing-user' WHERE id='new-session'; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec("PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if err := database.AdmitLocalSession(t.Context(), a); err == nil {
		t.Fatal("deferred commit failure accepted")
	}
	assertLocalAdmissionAbsent(t, database, a)
}

func TestBootstrapRejectsMissingInputs(t *testing.T) {
	database, _ := localAdmissionFixture(t, true)
	if err := database.Bootstrap(t.Context(), nil, nil); err == nil {
		t.Fatal("missing bootstrap input accepted")
	}
}

func loginAdmissionEvent(id, actorID string, at time.Time) model.SecurityEvent {
	return model.SecurityEvent{ID: id, Kind: model.SecurityEventLoginSucceeded, Severity: model.SecuritySeverityInfo,
		ActorUserID: actorID, ActorDisplayName: actorID, AuthMethod: model.SecurityAuthMethodPassword,
		SourceClass: model.SecuritySourceClassUnknown, SourceFamily: model.SecuritySourceFamilyIPv4, SourcePrefix: "192.0.2.0/24",
		BrowserFamily: "chrome", OSFamily: "linux", DeviceClass: model.SecurityDeviceClassDesktop, PolicyRevision: 1,
		SourceKey: "source:" + id, SourceKind: SecurityEventSourceKind, RoutingState: model.SecurityRoutingNotRouted,
		OccurredAt: at, RetainUntil: at.AddDate(0, 0, 180)}
}

func countRows(t *testing.T, store *AdminStore, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(t.Context(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAdmitLoginSessionCommitsSessionAuditAndDerivedSecurityData(t *testing.T) {
	store, admission := localAdmissionFixture(t, false)
	now := model.NowUTC()
	admission.Audit.CreatedAt = admission.Session.CreatedAt
	events := []model.SecurityEvent{loginAdmissionEvent("admit-event", admission.User.ID, now)}
	fingerprints := []SecurityFingerprint{{ActorUserID: admission.User.ID, Kind: SecurityFingerprintKindSource, Epoch: "epoch-1",
		Fingerprint: bytes.Repeat([]byte{9}, 32), SeenAt: now.UnixMilli(), RetainUntil: now.AddDate(0, 0, 180).UnixMilli()}}

	if err := store.AdmitLoginSession(t.Context(), LoginSessionAdmission{Session: admission, Events: events, Fingerprints: fingerprints}); err != nil {
		t.Fatalf("admit login session: %v", err)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM admin_sessions WHERE id=?", admission.Session.ID); got != 1 {
		t.Fatalf("admitted sessions = %d", got)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM security_events WHERE id='admit-event'"); got != 1 {
		t.Fatalf("security events = %d", got)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM security_login_fingerprints WHERE actor_user_id=?", admission.User.ID); got != 1 {
		t.Fatalf("security fingerprints = %d", got)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM admin_audit_events WHERE id=?", admission.Audit.ID); got != 1 {
		t.Fatalf("audit rows = %d", got)
	}
}

func TestAdmitLoginSessionTxUsesCallerTransactionAndRejectsInvalidAdmission(t *testing.T) {
	store, admission := localAdmissionFixture(t, false)
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := store.AdmitLoginSessionTx(t.Context(), tx, LoginSessionAdmission{Session: admission}); err != nil {
		t.Fatalf("admit login session in caller transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM admin_sessions WHERE id=?", admission.Session.ID); got != 1 {
		t.Fatalf("committed sessions = %d", got)
	}

	invalid := admission
	invalid.Session.ID, invalid.Session.TokenHash = "invalid-session", nil
	if err := store.AdmitLoginSession(t.Context(), LoginSessionAdmission{Session: invalid}); err == nil {
		t.Fatal("invalid admission was accepted")
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM admin_sessions WHERE id='invalid-session'"); got != 0 {
		t.Fatalf("rejected sessions = %d", got)
	}
}
