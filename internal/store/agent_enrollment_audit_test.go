package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
)

// enrollmentAuditRow 是测试读取到的一行入网审计事件。
type enrollmentAuditRow struct {
	action, actorID, actorUsername, outcome, detail, ip, userAgent, requestID string
}

// enrollmentAuditRows 按写入顺序返回全部入网审计行。
func enrollmentAuditRows(t *testing.T, fixture discoveryFixture) []enrollmentAuditRow {
	t.Helper()
	rows, err := fixture.store.db.Query(`SELECT action, COALESCE(actor_id, ''), actor_username, outcome,
		detail_json, ip_address, user_agent, request_id FROM admin_audit_events
		WHERE action LIKE 'agent.enrollment.%' ORDER BY rowid`)
	if err != nil {
		t.Fatalf("query enrollment audits: %v", err)
	}
	defer func() { _ = rows.Close() }()
	audits := make([]enrollmentAuditRow, 0)
	for rows.Next() {
		var row enrollmentAuditRow
		if err := rows.Scan(&row.action, &row.actorID, &row.actorUsername, &row.outcome, &row.detail,
			&row.ip, &row.userAgent, &row.requestID); err != nil {
			t.Fatalf("scan enrollment audit: %v", err)
		}
		audits = append(audits, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate enrollment audits: %v", err)
	}
	return audits
}

// clearEnrollmentAudits 清掉夹具自身产生的审计行，让每个用例只断言自己的动作。
func clearEnrollmentAudits(t *testing.T, fixture discoveryFixture) {
	t.Helper()
	if _, err := fixture.store.db.Exec(`DELETE FROM admin_audit_events WHERE action LIKE 'agent.enrollment.%'`); err != nil {
		t.Fatalf("clear enrollment audits: %v", err)
	}
}

// enrollmentAuditContext 构造一条管理端审计来源，字段全部为可断言的固定值。
func enrollmentAuditContext() EnrollmentAuditContext {
	return EnrollmentAuditContext{ActorID: "owner-1", ActorUsername: "owner", RequestID: "req-1",
		IPAddress: "10.0.0.9", UserAgent: "beat-test"}
}

// enrollmentTestSecret 生成一条可用的授权秘密，避免测试之间共享随机值。
func enrollmentTestSecret(t *testing.T) agentenrollment.Secret {
	t.Helper()
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate grant secret: %v", err)
	}
	return secret
}

// enrollmentTestPolicy 返回一条最小可用的授权策略。
func enrollmentTestPolicy(fixture discoveryFixture, name string) agentenrollment.GrantPolicy {
	return agentenrollment.GrantPolicy{Name: name, Mode: agentenrollment.ApprovalManual, MaximumEnrollments: 2,
		DefaultGroupID: fixture.grant.DefaultGroupID, SSHPort: 22,
		ExpiresAt: fixture.now.Add(time.Hour), Enabled: true}
}

func TestEnrollmentGrantAuditRecordsAdminActions(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	ctx := WithEnrollmentAuditContext(t.Context(), enrollmentAuditContext())
	grant, err := fixture.store.CreateGrant(ctx, EnrollmentGrantInput{
		Policy: enrollmentTestPolicy(fixture, "audit-grant"), Secret: enrollmentTestSecret(t)}, fixture.now)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	policy := enrollmentTestPolicy(fixture, "audit-grant-renamed")
	if _, err := fixture.store.UpdateGrant(ctx, grant.ID, EnrollmentGrantInput{Policy: policy}, fixture.now); err != nil {
		t.Fatalf("update grant: %v", err)
	}
	if _, err := fixture.store.RotateGrantSecret(ctx, grant.ID, enrollmentTestSecret(t), fixture.now); err != nil {
		t.Fatalf("rotate grant secret: %v", err)
	}
	if _, err := fixture.store.RevokeGrant(ctx, grant.ID, "owner-1", fixture.now); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}

	audits := enrollmentAuditRows(t, fixture)
	want := []string{EnrollmentActionGrantCreate, EnrollmentActionGrantUpdate,
		EnrollmentActionGrantRotate, EnrollmentActionGrantRevoke}
	if len(audits) != len(want) {
		t.Fatalf("audit count = %d, want %d: %#v", len(audits), len(want), audits)
	}
	for index, action := range want {
		row := audits[index]
		if row.action != action || row.actorID != "owner-1" || row.actorUsername != "owner" ||
			row.outcome != "success" || row.requestID != "req-1" || row.ip != "10.0.0.9" ||
			row.userAgent != "beat-test" {
			t.Fatalf("audit %d = %#v", index, row)
		}
	}
	if !strings.Contains(audits[0].detail, "audit-grant") {
		t.Fatalf("create detail = %s", audits[0].detail)
	}
	if strings.Contains(audits[2].detail, string(grant.SecretHash)) || strings.Contains(audits[0].detail, "secret_hash") {
		t.Fatalf("audit leaked grant secret material: %#v", audits)
	}
}

func TestEnrollmentDecisionAuditRecordsApprovalAndRejection(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	ctx := WithEnrollmentAuditContext(t.Context(), enrollmentAuditContext())
	request, _ := fixture.createRequest(t, fixture.now)
	approved, err := fixture.store.ApproveEnrollmentRequest(ctx, EnrollmentApproval{
		RequestID: request.ID, NodeName: "edge-audit", GroupID: fixture.grant.DefaultGroupID,
		Tags: []string{}, SSHPort: 22, ActorID: "owner-1"}, fixture.now)
	if err != nil {
		t.Fatalf("approve request: %v", err)
	}
	second := fixture
	second.key = []byte(strings.Repeat("\x2a", 32))
	rejected, _ := second.createRequest(t, fixture.now.Add(time.Minute))
	if _, err := fixture.store.RejectEnrollmentRequest(ctx, EnrollmentRejection{
		RequestID: rejected.ID, ActorID: "owner-1", Reason: "lab"}, fixture.now); err != nil {
		t.Fatalf("reject request: %v", err)
	}

	audits := enrollmentAuditRows(t, fixture)
	if len(audits) != 4 {
		t.Fatalf("decision audits = %#v", audits)
	}
	approval, rejection := audits[1], audits[3]
	if approval.action != EnrollmentActionRequestApprove || rejection.action != EnrollmentActionRequestReject {
		t.Fatalf("decision audits = %#v", audits)
	}
	if approval.actorID != "owner-1" || !strings.Contains(approval.detail, approved.ReservedNodeID.String) {
		t.Fatalf("approval audit = %#v", approval)
	}
	if rejection.actorID != "owner-1" || !strings.Contains(rejection.detail, `"reason":"lab"`) {
		t.Fatalf("rejection audit = %#v", rejection)
	}
}

func TestEnrollmentProtocolAuditUsesAnonymousActor(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	request, token := fixture.createRequest(t, fixture.now)
	resumed, _ := fixture.createRequest(t, fixture.now.Add(time.Minute))
	if resumed.ID != request.ID {
		t.Fatalf("resume created a second request: %s", resumed.ID)
	}
	ticket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatalf("generate ticket: %v", err)
	}
	adminCtx := WithEnrollmentAuditContext(t.Context(), enrollmentAuditContext())
	if _, err := fixture.store.ApproveEnrollmentRequest(adminCtx, EnrollmentApproval{
		RequestID: request.ID, NodeName: "edge-protocol", GroupID: fixture.grant.DefaultGroupID,
		Tags: []string{}, SSHPort: 22, ActorID: "owner-1"}, fixture.now); err != nil {
		t.Fatalf("approve request: %v", err)
	}
	if _, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, ticket, fixture.now); err != nil {
		t.Fatalf("rotate ticket: %v", err)
	}
	nodeToken, err := agentcredential.Generate()
	if err != nil {
		t.Fatalf("generate node token: %v", err)
	}
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, nodeToken, fixture.now); err != nil {
		t.Fatalf("claim: %v", err)
	}
	_ = token

	audits := enrollmentAuditRows(t, fixture)
	want := []string{EnrollmentActionRequestCreate, EnrollmentActionRequestResume,
		EnrollmentActionRequestApprove, EnrollmentActionClaim}
	if len(audits) != len(want) {
		t.Fatalf("audit count = %d, want %d: %#v", len(audits), len(want), audits)
	}
	for index, action := range want {
		if audits[index].action != action {
			t.Fatalf("audit %d = %#v", index, audits[index])
		}
	}
	for _, index := range []int{0, 1, 3} {
		if audits[index].actorID != "" || audits[index].actorUsername != "agent" {
			t.Fatalf("anonymous audit %d = %#v", index, audits[index])
		}
	}
	if audits[2].actorID != "owner-1" || audits[2].actorUsername != "owner" {
		t.Fatalf("approval audit = %#v", audits[2])
	}
	if !strings.Contains(audits[0].detail, "edge-01.example.com") {
		t.Fatalf("request create detail = %s", audits[0].detail)
	}
	if strings.Contains(audits[3].detail, "beat_agent_v1") || strings.Contains(audits[3].detail, ticket.Plaintext) {
		t.Fatalf("claim audit leaked credentials: %s", audits[3].detail)
	}
}

func TestEnrollmentConflictAuditRecordsReasonCode(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	policy := enrollmentTestPolicy(fixture, "auto-conflict")
	policy.Mode = agentenrollment.ApprovalAutomatic
	policy.NodeNamePrefix = "edge-"
	policy.HostnamePattern = "^absent-"
	grant, err := fixture.store.CreateGrant(t.Context(), EnrollmentGrantInput{
		Policy: policy, Secret: enrollmentTestSecret(t)}, fixture.now)
	if err != nil {
		t.Fatalf("create automatic grant: %v", err)
	}
	conflict := fixture
	conflict.grant = grant
	conflict.key = []byte(strings.Repeat("\x31", 32))
	request, _ := conflict.createRequest(t, fixture.now)
	if request.Status != agentenrollment.RequestPending {
		t.Fatalf("conflicting request status = %s", request.Status)
	}

	audits := enrollmentAuditRows(t, fixture)
	found := false
	for _, row := range audits {
		if row.action == EnrollmentActionRequestConflict {
			found = true
			if !strings.Contains(row.detail, "policy_conflict") || row.actorUsername != "agent" {
				t.Fatalf("conflict audit = %#v", row)
			}
		}
	}
	if !found {
		t.Fatalf("conflict audit missing: %#v", audits)
	}
}

func TestEnrollmentAuditFailureRollsBackGrantCreate(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	ctx := WithEnrollmentAuditContext(t.Context(), enrollmentAuditContext())
	if _, err := fixture.store.db.Exec("DROP TABLE admin_audit_events"); err != nil {
		t.Fatalf("drop audit table: %v", err)
	}
	if _, err := fixture.store.CreateGrant(ctx, EnrollmentGrantInput{
		Policy: enrollmentTestPolicy(fixture, "audit-disabled"), Secret: enrollmentTestSecret(t)}, fixture.now); err == nil {
		t.Fatal("create grant without audit table unexpectedly succeeded")
	}
	var count int
	if err := fixture.store.db.QueryRow(`SELECT COUNT(*) FROM agent_enrollment_grants WHERE name = ?`,
		"audit-disabled").Scan(&count); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if count != 0 {
		t.Fatalf("grant survived a failed audit write: %d", count)
	}
}

// 断言审计字段在超长输入下被截断，避免无界写入撑大审计表。
func TestEnrollmentAuditTruncatesUnboundedFields(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	ctx := WithEnrollmentAuditContext(context.Background(), EnrollmentAuditContext{
		ActorID: strings.Repeat("a", 200), ActorUsername: strings.Repeat("b", 200),
		RequestID: strings.Repeat("c", 200), IPAddress: strings.Repeat("d", 200),
		UserAgent: strings.Repeat("e", 700)})
	if _, err := fixture.store.CreateGrant(ctx, EnrollmentGrantInput{
		Policy: enrollmentTestPolicy(fixture, "audit-truncated"), Secret: enrollmentTestSecret(t)}, fixture.now); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	audits := enrollmentAuditRows(t, fixture)
	if len(audits) != 1 {
		t.Fatalf("audit count = %d", len(audits))
	}
	if len(audits[0].actorID) != enrollmentAuditRequestIDLimit ||
		len(audits[0].actorUsername) != enrollmentAuditRequestIDLimit ||
		len(audits[0].requestID) != enrollmentAuditRequestIDLimit ||
		len(audits[0].ip) != enrollmentAuditIPLimit ||
		len(audits[0].userAgent) != enrollmentAuditUserAgentLimit {
		t.Fatalf("unbounded audit fields = %#v", audits[0])
	}
}

func TestEnrollmentCleanupAuditsExpiredRequests(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	clearEnrollmentAudits(t, fixture)
	request, _ := fixture.createRequest(t, fixture.now)
	if _, err := fixture.store.CleanupEnrollmentState(t.Context(), fixture.now.Add(4*24*time.Hour)); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	audits := enrollmentAuditRows(t, fixture)
	if len(audits) != 2 {
		t.Fatalf("audit rows = %#v", audits)
	}
	expiry := audits[1]
	if expiry.action != EnrollmentActionRequestExpire || expiry.actorUsername != "agent" ||
		!strings.Contains(expiry.detail, `"expired_requests":1`) {
		t.Fatalf("expiry audit = %#v", expiry)
	}
	// 干净的清理回合不再写审计行。
	if _, err := fixture.store.CleanupEnrollmentState(t.Context(), fixture.now.Add(5*24*time.Hour)); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if rows := enrollmentAuditRows(t, fixture); len(rows) != 2 {
		t.Fatalf("cleanup wrote extra audit rows: %#v", rows)
	}
	if request.ID == "" {
		t.Fatal("fixture request is missing")
	}
}
