package store

import (
	"database/sql"
	"testing"
	"time"
)

func securityThresholdBump(seed byte, now time.Time) ThresholdBump {
	return ThresholdBump{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(seed), Now: now,
		Window: time.Minute, Limit: 3, Lockout: time.Minute}
}

func seedSecurityThresholds(t *testing.T, database *SQLiteStore, count int, lastSeen int64) {
	t.Helper()
	if _, err := database.DB.ExecContext(t.Context(), `WITH RECURSIVE seq(n) AS (
		VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<?)
		INSERT INTO security_login_thresholds(key_kind, key_hash, window_started_at, attempt_count,
			lock_expires_at, last_seen_at, last_emitted_at)
		SELECT 'source', randomblob(32), 1, 1, NULL, ?, NULL FROM seq`, count, lastSeen); err != nil {
		t.Fatal(err)
	}
}

// TestAdmitLoginFailureStoreFaults 覆盖失败准入的事务、审计与提交失败。
func TestAdmitLoginFailureStoreFaults(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	admission := func(id string) LoginFailureAdmission {
		return LoginFailureAdmission{Audit: securityTestAudit(now, id),
			Thresholds: []ThresholdBump{securityThresholdBump(1, now)}}
	}
	t.Run("closed database", func(t *testing.T) {
		closed := NewAdminStore(closedSQLiteStore(t).DB)
		if _, err := closed.AdmitLoginFailure(t.Context(), admission("cov-closed")); err == nil {
			t.Fatal("closed database admitted a login failure")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		database := setupTestDB(t)
		installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_threshold_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if _, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), admission("cov-audit")); err == nil {
			t.Fatal("blocked threshold audit was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		database := setupTestDB(t)
		installEnrollmentFaultStatement(t, database, `CREATE TABLE cov_threshold_child(
			audit TEXT REFERENCES admin_audit_events(id) DEFERRABLE INITIALLY DEFERRED)`)
		installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_threshold_commit
			AFTER INSERT ON admin_audit_events BEGIN INSERT INTO cov_threshold_child VALUES('missing'); END`)
		if _, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), admission("cov-commit")); err == nil {
			t.Fatal("deferred threshold commit failure was accepted")
		}
	})
	t.Run("read failure", func(t *testing.T) {
		database := setupTestDB(t)
		installEnrollmentFaultStatement(t, database,
			"ALTER TABLE security_login_thresholds RENAME TO cov_threshold_backup")
		if _, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), admission("cov-read")); err == nil {
			t.Fatal("unreadable threshold window was accepted")
		}
	})
}

// installEnrollmentFaultStatement 执行一段注入故障的 SQL。
func installEnrollmentFaultStatement(t *testing.T, database *SQLiteStore, statement string) {
	t.Helper()
	if _, err := database.DB.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

// TestThresholdWindowWriteFaults 覆盖窗口更新、插入与过期清理的写入失败。
func TestThresholdWindowWriteFaults(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	t.Run("update blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewAdminStore(database.DB)
		bump := securityThresholdBump(2, now)
		if _, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
			Audit: securityTestAudit(now, "cov-window"), Thresholds: []ThresholdBump{bump}}); err != nil {
			t.Fatal(err)
		}
		installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_threshold_update
			BEFORE UPDATE ON security_login_thresholds BEGIN SELECT RAISE(ABORT,'update blocked'); END`)
		if _, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
			Audit:      securityTestAudit(now.Add(time.Second), "cov-window-2"),
			Thresholds: []ThresholdBump{bump}}); err == nil {
			t.Fatal("blocked threshold update was accepted")
		}
	})
	t.Run("insert blocked", func(t *testing.T) {
		database := setupTestDB(t)
		installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_threshold_insert
			BEFORE INSERT ON security_login_thresholds BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		if _, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), LoginFailureAdmission{
			Audit: securityTestAudit(now, "cov-insert"), Thresholds: []ThresholdBump{securityThresholdBump(3, now)}}); err == nil {
			t.Fatal("blocked threshold insert was accepted")
		}
	})
	t.Run("expire blocked", func(t *testing.T) {
		database := setupTestDB(t)
		seedSecurityThresholds(t, database, 1, now.Add(-72*time.Hour).UnixMilli())
		installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_threshold_expire
			BEFORE DELETE ON security_login_thresholds BEGIN SELECT RAISE(ABORT,'expire blocked'); END`)
		if _, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), LoginFailureAdmission{
			Audit: securityTestAudit(now, "cov-expire"), Thresholds: []ThresholdBump{securityThresholdBump(4, now)}}); err == nil {
			t.Fatal("blocked threshold expiry was accepted")
		}
	})
}

// TestThresholdTrimWorkload 覆盖超过上限行数时的裁剪路径。
func TestThresholdTrimWorkload(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	database := setupTestDB(t)
	seedSecurityThresholds(t, database, securityThresholdMaximumRows+1, now.UnixMilli())
	if _, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(now, "cov-trim"), Thresholds: []ThresholdBump{securityThresholdBump(5, now)}}); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM security_login_thresholds").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total > securityThresholdMaximumRows {
		t.Fatalf("threshold rows after trim = %d", total)
	}
}

// TestThresholdTrimBlocked 覆盖裁剪写入被阻断时的失败关闭。
func TestThresholdTrimBlocked(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	database := setupTestDB(t)
	seedSecurityThresholds(t, database, securityThresholdMaximumRows+1, now.UnixMilli())
	installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_threshold_trim
		BEFORE DELETE ON security_login_thresholds BEGIN SELECT RAISE(ABORT,'trim blocked'); END`)
	_, err := NewAdminStore(database.DB).AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(now, "cov-trim-blocked"), Thresholds: []ThresholdBump{securityThresholdBump(6, now)}})
	if err == nil {
		t.Fatal("blocked threshold trim was accepted")
	}
}

// TestMarkThresholdEmittedFaults 覆盖下发标记的写入失败。
func TestMarkThresholdEmittedFaults(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	database := setupTestDB(t)
	store := NewAdminStore(database.DB)
	bump := securityThresholdBump(7, now)
	outcomes, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
		Audit: securityTestAudit(now, "cov-emit"), Thresholds: []ThresholdBump{bump}})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := beginWriteTx(t.Context(), database.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), `CREATE TRIGGER cov_threshold_mark
		BEFORE UPDATE ON security_login_thresholds BEGIN SELECT RAISE(ABORT,'mark blocked'); END`); err != nil {
		t.Fatal(err)
	}
	key := ThresholdKey{KeyKind: outcomes[0].KeyKind, KeyHash: outcomes[0].KeyHash, WindowAt: outcomes[0].WindowAt}
	if marked, err := store.MarkThresholdEmittedTx(t.Context(), tx, key, now); err == nil || marked {
		t.Fatalf("blocked mark = %v / %v", marked, err)
	}
}

// TestEmitThresholdEventFaults 覆盖无队列下发路径的事务与提交失败。
func TestEmitThresholdEventFaults(t *testing.T) {
	now := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	key := ThresholdKey{KeyKind: SecurityFingerprintKindSource, KeyHash: testSecurityKey(8), WindowAt: now}
	t.Run("closed database", func(t *testing.T) {
		closed := NewAdminStore(closedSQLiteStore(t).DB)
		if marked, err := closed.EmitThresholdEvent(t.Context(), key,
			testThresholdEvent("cov-closed-event", now), now); err == nil || marked {
			t.Fatalf("closed database emit = %v / %v", marked, err)
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		database := setupTestDB(t)
		store := NewAdminStore(database.DB)
		bump := securityThresholdBump(9, now)
		bump.Limit = 1
		outcomes, err := store.AdmitLoginFailure(t.Context(), LoginFailureAdmission{
			Audit: securityTestAudit(now, "cov-emit-commit"), Thresholds: []ThresholdBump{bump}})
		if err != nil || !outcomes[0].ShouldEmit {
			t.Fatalf("threshold outcome = %+v / %v", outcomes, err)
		}
		installEnrollmentFaultStatement(t, database, `CREATE TABLE cov_event_child(
			event TEXT REFERENCES security_events(id) DEFERRABLE INITIALLY DEFERRED)`)
		installEnrollmentFaultStatement(t, database, `CREATE TRIGGER cov_event_commit
			AFTER INSERT ON security_events BEGIN INSERT INTO cov_event_child VALUES('missing'); END`)
		emitKey := ThresholdKey{KeyKind: outcomes[0].KeyKind, KeyHash: outcomes[0].KeyHash, WindowAt: outcomes[0].WindowAt}
		if marked, err := store.EmitThresholdEvent(t.Context(), emitKey,
			testThresholdEvent("cov-commit-event", now), now); err == nil || marked {
			t.Fatalf("deferred emit commit = %v / %v", marked, err)
		}
	})
}

// TestLockValueKeepsExistingLock 覆盖保留既有锁定时间的一支。
func TestLockValueKeepsExistingLock(t *testing.T) {
	existing := sql.NullInt64{Int64: 1_700_000_000_000, Valid: true}
	kept := lockValue(ThresholdOutcome{}, existing)
	if kept == nil || !kept.Equal(time.UnixMilli(existing.Int64).UTC()) {
		t.Fatalf("lock value = %v", kept)
	}
	if lockValue(ThresholdOutcome{}, sql.NullInt64{}) != nil {
		t.Fatal("empty lock value was kept")
	}
}
