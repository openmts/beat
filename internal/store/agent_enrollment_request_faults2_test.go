package store

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

func covEnrollmentRequestInput(t *testing.T, fixture discoveryFixture, now time.Time) EnrollmentRequestInput {
	t.Helper()
	token, err := agentenrollment.GenerateRequestToken()
	if err != nil {
		t.Fatal(err)
	}
	return EnrollmentRequestInput{GrantID: fixture.grant.ID, Fingerprint: enrollmentFingerprint(t, fixture.key),
		PublicKey: fixture.key, Token: token, Metadata: fixture.metadata(),
		SourceIP: "10.0.0.5", Now: now}
}

// TestEnrollmentRequestCreateFaults 覆盖创建请求的事务、查询、审计、提交与回读失败。
func TestEnrollmentRequestCreateFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		closed := NewAgentEnrollmentStore(closedSQLiteStore(t).DB)
		if _, _, err := closed.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("closed database created a request")
		}
	})
	t.Run("request ledger unreadable", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		if _, err := fixture.store.db.ExecContext(t.Context(),
			"ALTER TABLE agent_enrollment_requests RENAME TO cov_requests_backup"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("unreadable request ledger was accepted")
		}
	})
	t.Run("insert blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_request_insert
			BEFORE INSERT ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("blocked request insert was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_request_audit
			BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("blocked request audit was accepted")
		}
	})
	t.Run("deferred commit", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installCustomMetricCommitFault(t, fixture.store.db, "INSERT ON agent_enrollment_requests")
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("deferred request commit failure was accepted")
		}
	})
	t.Run("reread after commit", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_request_reread
			AFTER INSERT ON admin_audit_events
			BEGIN DELETE FROM agent_enrollment_requests WHERE status = 'pending'; END`)
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("vanished request reread was accepted")
		}
	})
}

// TestEnrollmentRequestResumeFaults 覆盖复用请求时令牌轮换的写入与命中失败。
func TestEnrollmentRequestResumeFaults(t *testing.T) {
	t.Run("rotate blocked", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_request_rotate
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(ABORT,'rotate blocked'); END`)
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("blocked token rotation was accepted")
		}
	})
	t.Run("rotate ignored", func(t *testing.T) {
		fixture := newEnrollmentFixture(t)
		fixture.createRequest(t, fixture.now)
		installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_request_ignore
			BEFORE UPDATE ON agent_enrollment_requests BEGIN SELECT RAISE(IGNORE); END`)
		if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
			covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
			t.Fatal("ignored token rotation was accepted")
		}
	})
}

// TestAutomaticEnrollmentRequestFaults 覆盖自动审批冲突记录被审计阻断时的失败关闭。
func TestAutomaticEnrollmentRequestFaults(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	fixture.grant = mustCreateAutomaticGrant(t, fixture, func(policy *agentenrollment.GrantPolicy) {
		policy.HostnamePattern = "^db-"
	})
	installEnrollmentTrigger(t, fixture, `CREATE TRIGGER cov_auto_conflict_audit
		BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'conflict audit blocked'); END`)
	if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(),
		covEnrollmentRequestInput(t, fixture, fixture.now)); err == nil {
		t.Fatal("blocked conflict audit was accepted")
	}
}

// TestEnrollmentCursorDecodingEdges 覆盖游标解码的三条拒绝分支。
func TestEnrollmentCursorDecodingEdges(t *testing.T) {
	encode := func(payload string) string { return base64.RawURLEncoding.EncodeToString([]byte(payload)) }
	if _, _, ok := decodeEnrollmentCursor("not-base64??"); ok {
		t.Fatal("invalid base64 cursor decoded")
	}
	if _, _, ok := decodeEnrollmentCursor(encode("no-separator")); ok {
		t.Fatal("cursor without separator decoded")
	}
	if _, _, ok := decodeEnrollmentCursor(encode("not-a-time|request-1")); ok {
		t.Fatal("cursor with invalid timestamp decoded")
	}
	at, id, ok := decodeEnrollmentCursor(encode("2026-10-15T12:00:00Z|request-1"))
	if !ok || id != "request-1" || !at.Equal(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("valid cursor = %v %q %v", at, id, ok)
	}
}
