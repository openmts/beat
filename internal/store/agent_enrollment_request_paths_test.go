package store

import (
	"bytes"
	"errors"
	"testing"

	"github.com/beat/backend/internal/agentenrollment"
)

func TestEnrollmentRequestCreateGuards(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	secret, err := agentenrollment.GenerateRequestToken()
	if err != nil {
		t.Fatalf("generate request token: %v", err)
	}
	invalid := agentenrollment.Metadata{Hostname: "edge-01", OS: "linux", Architecture: "amd64",
		AgentVersion: "1.0.0", SSHPort: 0, InstallationNonce: "nonce"}
	if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(), EnrollmentRequestInput{
		GrantID: fixture.grant.ID, Fingerprint: fixture.key, PublicKey: fixture.key, Token: secret,
		Metadata: invalid, SourceIP: "10.0.0.5", Now: fixture.now}); err == nil {
		t.Fatal("invalid metadata was accepted")
	}
	valid := fixture.metadata()
	if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(), EnrollmentRequestInput{
		GrantID: "missing-grant", Fingerprint: fixture.key, PublicKey: fixture.key, Token: secret,
		Metadata: valid, SourceIP: "10.0.0.5", Now: fixture.now}); !errors.Is(err, ErrEnrollmentGrantUnavailable) {
		t.Fatalf("unknown grant error = %v", err)
	}
	disabledPolicy := grantPolicy(fixture.grant, "disabled-automatic")
	disabledPolicy.Enabled = false
	disabled := mustCreateGrant(t, fixture, disabledPolicy)
	if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(), EnrollmentRequestInput{
		GrantID: disabled.ID, Fingerprint: fixture.key, PublicKey: fixture.key, Token: secret,
		Metadata: valid, SourceIP: "10.0.0.5", Now: fixture.now}); !errors.Is(err, ErrEnrollmentGrantUnavailable) {
		t.Fatalf("disabled grant error = %v", err)
	}
	request, _ := fixture.createRequest(t, fixture.now)
	otherKey := bytes.Repeat([]byte{0x22}, 32)
	if _, _, err := fixture.store.CreateOrResumeRequest(t.Context(), EnrollmentRequestInput{
		GrantID: fixture.grant.ID, Fingerprint: request.KeyFingerprint, PublicKey: otherKey, Token: secret,
		Metadata: valid, SourceIP: "10.0.0.5", Now: fixture.now}); !errors.Is(err, ErrEnrollmentRequestUnavailable) {
		t.Fatalf("public key mismatch error = %v", err)
	}
}

func TestEnrollmentRequestLookupAndPaging(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	if _, err := fixture.store.EnrollmentRequest(t.Context(), "missing-request"); !errors.Is(err, ErrEnrollmentRequestNotFound) {
		t.Fatalf("unknown request error = %v", err)
	}
	request, _ := fixture.createRequest(t, fixture.now)
	pending, err := fixture.store.ListEnrollmentRequests(t.Context(), EnrollmentRequestFilter{
		Status: agentenrollment.RequestPending, GrantID: fixture.grant.ID, Limit: 1})
	if err != nil || len(pending.Requests) != 1 || pending.Requests[0].ID != request.ID || pending.NextCursor == "" {
		t.Fatalf("pending page = %+v err = %v", pending, err)
	}
	unknownStatus, err := fixture.store.ListEnrollmentRequests(t.Context(),
		EnrollmentRequestFilter{Status: "not-a-status", Limit: 10_000})
	if err != nil || len(unknownStatus.Requests) != 1 {
		t.Fatalf("unknown status page = %+v err = %v", unknownStatus, err)
	}
	rejected, err := fixture.store.ListEnrollmentRequests(t.Context(),
		EnrollmentRequestFilter{Status: agentenrollment.RequestRejected})
	if err != nil || len(rejected.Requests) != 0 {
		t.Fatalf("rejected page = %+v err = %v", rejected, err)
	}
	if _, err := fixture.store.ListEnrollmentRequests(t.Context(),
		EnrollmentRequestFilter{Cursor: "not-base64??"}); err != nil {
		t.Fatalf("broken cursor error = %v", err)
	}
	next, err := fixture.store.ListEnrollmentRequests(t.Context(),
		EnrollmentRequestFilter{Cursor: pending.NextCursor})
	if err != nil || len(next.Requests) != 0 {
		t.Fatalf("cursor page = %+v err = %v", next, err)
	}
	otherGrant, err := fixture.store.ListEnrollmentRequests(t.Context(),
		EnrollmentRequestFilter{GrantID: "missing-grant"})
	if err != nil || len(otherGrant.Requests) != 0 {
		t.Fatalf("other grant page = %+v err = %v", otherGrant, err)
	}
}

// mustCreateAutomaticGrant 建一条自动审批授权，mutate 用于逐个用例调整策略。
func mustCreateAutomaticGrant(t *testing.T, fixture discoveryFixture,
	mutate func(*agentenrollment.GrantPolicy)) EnrollmentGrant {
	t.Helper()
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate grant secret: %v", err)
	}
	policy := grantPolicy(fixture.grant, "auto-"+t.Name())
	policy.Mode = agentenrollment.ApprovalAutomatic
	policy.NodeNamePrefix = "edge"
	policy.MaximumEnrollments = 10
	if mutate != nil {
		mutate(&policy)
	}
	grant, err := fixture.store.CreateGrant(t.Context(),
		EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner-1"}, fixture.now)
	if err != nil {
		t.Fatalf("create automatic grant: %v", err)
	}
	return grant
}

func TestAutomaticEnrollmentRecordsPolicyConflict(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	fixture.grant = mustCreateAutomaticGrant(t, fixture, func(policy *agentenrollment.GrantPolicy) {
		policy.HostnamePattern = "^db-"
	})
	request, _ := fixture.createRequest(t, fixture.now)
	if request.Status != agentenrollment.RequestPending || request.RejectionReason != enrollmentReasonPolicyConflict {
		t.Fatalf("conflict request = %+v", request)
	}
	var audit int
	if err := fixture.store.db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM admin_audit_events WHERE action = ?", string(EnrollmentActionRequestConflict)).Scan(&audit); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if audit != 1 {
		t.Fatalf("conflict audit count = %d", audit)
	}
}

func TestAutomaticEnrollmentRecordsNameConflict(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	fixture.grant = mustCreateAutomaticGrant(t, fixture, nil)
	if _, err := fixture.nodes.UpsertNode(t.Context(), "edge-edge-01-example-com", "10.0.0.9", 22); err != nil {
		t.Fatalf("seed conflicting node: %v", err)
	}
	request, _ := fixture.createRequest(t, fixture.now)
	if request.Status != agentenrollment.RequestPending || request.RejectionReason != enrollmentReasonNameConflict {
		t.Fatalf("name conflict request = %+v", request)
	}
}

func TestAutomaticEnrollmentUsesSourceIPAsHost(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	fixture.grant = mustCreateAutomaticGrant(t, fixture, func(policy *agentenrollment.GrantPolicy) {
		policy.UseSourceIPAsHost = true
	})
	request, _ := fixture.createRequest(t, fixture.now)
	if request.Status != agentenrollment.RequestApproved {
		t.Fatalf("automatic request = %+v", request)
	}
	var host string
	if err := fixture.store.db.QueryRowContext(t.Context(),
		"SELECT host FROM nodes WHERE id = ?", request.ReservedNodeID.String).Scan(&host); err != nil {
		t.Fatalf("read reserved node: %v", err)
	}
	if host != "10.0.0.5" {
		t.Fatalf("reserved host = %s", host)
	}
}
