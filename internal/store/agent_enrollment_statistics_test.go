package store

import (
	"bytes"
	"database/sql"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

// enrollmentStatisticsGrant 追加一条授权，返回其 ID；策略由调用方补齐。
func enrollmentStatisticsGrant(t *testing.T, fixture discoveryFixture, name string,
	mutate func(*agentenrollment.GrantPolicy)) string {
	t.Helper()
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate grant secret: %v", err)
	}
	policy := agentenrollment.GrantPolicy{Name: name, Mode: agentenrollment.ApprovalManual, MaximumEnrollments: 4,
		DefaultGroupID: fixture.grant.DefaultGroupID, SSHPort: 22,
		ExpiresAt: fixture.now.Add(time.Hour), Enabled: true}
	if mutate != nil {
		mutate(&policy)
	}
	grant, err := fixture.store.CreateGrant(t.Context(), EnrollmentGrantInput{
		Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now)
	if err != nil {
		t.Fatalf("create grant %s: %v", name, err)
	}
	return grant.ID
}

func TestEnrollmentStatisticsEmptyStore(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	if _, err := fixture.store.db.Exec("DELETE FROM agent_enrollment_grants"); err != nil {
		t.Fatalf("clear grants: %v", err)
	}
	statistics, err := fixture.store.EnrollmentStatistics(t.Context(), fixture.now)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if statistics != (EnrollmentStatistics{}) {
		t.Fatalf("empty statistics = %#v", statistics)
	}
}

func TestEnrollmentStatisticsAggregatesRequestsAndGrants(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	approved, _ := fixture.createRequest(t, fixture.now.Add(-90*time.Minute))
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), EnrollmentApproval{
		RequestID: approved.ID, NodeName: "edge-approved", GroupID: fixture.grant.DefaultGroupID,
		Tags: []string{}, SSHPort: 22, ActorID: "owner-1"}, fixture.now); err != nil {
		t.Fatalf("approve request: %v", err)
	}
	pendingFixture := fixture
	pendingFixture.key = bytes.Repeat([]byte{0x22}, 32)
	pending, _ := pendingFixture.createRequest(t, fixture.now)
	rejectedFixture := fixture
	rejectedFixture.key = bytes.Repeat([]byte{0x33}, 32)
	rejected, _ := rejectedFixture.createRequest(t, fixture.now)
	if _, err := fixture.store.RejectEnrollmentRequest(t.Context(), EnrollmentRejection{
		RequestID: rejected.ID, ActorID: "owner-1", Reason: "lab"}, fixture.now); err != nil {
		t.Fatalf("reject request: %v", err)
	}

	enrollmentStatisticsGrant(t, fixture, "expiring", nil)
	revoked := enrollmentStatisticsGrant(t, fixture, "revoked", nil)
	if _, err := fixture.store.RevokeGrant(t.Context(), revoked, "owner-1", fixture.now); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	enrollmentStatisticsGrant(t, fixture, "disabled", func(policy *agentenrollment.GrantPolicy) {
		policy.Enabled = false
		policy.ExpiresAt = fixture.now.Add(24 * time.Hour)
	})

	statistics, err := fixture.store.EnrollmentStatistics(t.Context(), fixture.now)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if statistics.Pending != 1 || statistics.Approved != 1 || statistics.Rejected != 1 {
		t.Fatalf("request counts = %#v", statistics)
	}
	if statistics.Expired != 0 || statistics.Claimed != 0 {
		t.Fatalf("unexpected terminal counts = %#v", statistics)
	}
	// 已审批但未认领的请求仍然占用队列，因此最老未决时间来自先创建的那条。
	if statistics.OldestOpenRequestAt.Time.UTC() != approved.FirstRequestAt.UTC() {
		t.Fatalf("oldest open request = %v, want %v", statistics.OldestOpenRequestAt.Time, approved.FirstRequestAt)
	}
	if pending.FirstRequestAt.UTC() != fixture.now.UTC() {
		t.Fatalf("pending fixture start = %v", pending.FirstRequestAt)
	}
	if statistics.OldestOpenRequestAge != 90*time.Minute {
		t.Fatalf("oldest open age = %v", statistics.OldestOpenRequestAge)
	}
	if statistics.ActiveGrants != 2 || statistics.ExpiredGrants != 0 ||
		statistics.DisabledGrants != 1 || statistics.RevokedGrants != 1 {
		t.Fatalf("grant counts = %#v", statistics)
	}

	later := fixture.now.Add(2 * time.Hour)
	expired, err := fixture.store.EnrollmentStatistics(t.Context(), later)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if expired.ActiveGrants != 1 || expired.ExpiredGrants != 1 ||
		expired.DisabledGrants != 1 || expired.RevokedGrants != 1 {
		t.Fatalf("expired grant counts = %#v", expired)
	}
	if expired.OldestOpenRequestAge != 3*time.Hour+30*time.Minute {
		t.Fatalf("later oldest open age = %v", expired.OldestOpenRequestAge)
	}
}

func TestOldestOpenRequestAgeClampsClockSkew(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if age := oldestOpenRequestAge(sql.NullTime{Time: now.Add(time.Hour), Valid: true}, now); age != 0 {
		t.Fatalf("future timestamp produced %v", age)
	}
}
