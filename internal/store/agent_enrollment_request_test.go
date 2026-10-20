package store

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
)

type discoveryFixture struct {
	store  *AgentEnrollmentStore
	nodes  *NodeStore
	grant  EnrollmentGrant
	secret agentenrollment.Secret
	now    time.Time
	key    []byte
}

func newEnrollmentFixture(t *testing.T) discoveryFixture {
	t.Helper()
	database := setupTestDB(t)
	groups := NewGroupStore(database.DB)
	group, err := groups.CreateGroup(t.Context(), "enrollment-target")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate grant secret: %v", err)
	}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	store := NewAgentEnrollmentStore(database.DB)
	grant, err := store.CreateGrant(t.Context(), EnrollmentGrantInput{
		Policy: agentenrollment.GrantPolicy{
			Name: "edge-fleet", Mode: agentenrollment.ApprovalManual, MaximumEnrollments: 2,
			DefaultGroupID: group.ID, SSHPort: 22, ExpiresAt: now.Add(24 * time.Hour), Enabled: true,
		},
		Secret: secret, ActorID: "owner-1",
	}, now)
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	return discoveryFixture{store: store, nodes: NewNodeStore(database.DB), grant: grant, secret: secret,
		now: now, key: bytes.Repeat([]byte{0x11}, 32)}
}

func (fixture discoveryFixture) metadata() agentenrollment.Metadata {
	return agentenrollment.Metadata{Hostname: "edge-01.example.com", AdvertisedHost: "10.0.0.5",
		OS: "linux", Architecture: "amd64", AgentVersion: "1.2.3", SSHPort: 22, InstallationNonce: "nonce-1"}
}

// createRequest 走完整的挑战与请求创建流程，返回请求与一次性令牌明文。
func (fixture discoveryFixture) createRequest(t *testing.T, now time.Time) (EnrollmentRequest, string) {
	t.Helper()
	return createEnrollmentRequest(t, fixture, now, fixture.metadata())
}

func createEnrollmentRequest(t *testing.T, fixture discoveryFixture, now time.Time,
	metadata agentenrollment.Metadata) (EnrollmentRequest, string) {
	t.Helper()
	challengeBytes, err := agentenrollment.GenerateChallenge()
	if err != nil {
		t.Fatalf("generate challenge: %v", err)
	}
	digest := enrollmentDigest(t, `{"hostname":"edge-01.example.com"}`)
	fingerprint := enrollmentFingerprint(t, fixture.key)
	challengeHash := enrollmentDigestBytes(t, challengeBytes)
	challenge, err := fixture.store.CreateChallenge(t.Context(), EnrollmentChallengeInput{
		GrantID: fixture.grant.ID, ChallengeHash: challengeHash,
		KeyFingerprint: fingerprint, MetadataDigest: digest,
		SourceIP: "10.0.0.5", Now: now, ExpiresAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	if _, err := fixture.store.ConsumeChallenge(t.Context(), EnrollmentChallengeProof{
		ID: challenge.ID, Challenge: challengeBytes, KeyFingerprint: fingerprint,
		MetadataDigest: digest, SourceIP: "10.0.0.5", Now: now}); err != nil {
		t.Fatalf("consume challenge: %v", err)
	}
	token, err := agentenrollment.GenerateRequestToken()
	if err != nil {
		t.Fatalf("generate request token: %v", err)
	}
	request, _, err := fixture.store.CreateOrResumeRequest(t.Context(), EnrollmentRequestInput{
		GrantID: fixture.grant.ID, Fingerprint: fingerprint,
		PublicKey: fixture.key, Token: token, Metadata: metadata, SourceIP: "10.0.0.5", Now: now})
	if err != nil {
		t.Fatalf("create enrollment request: %v", err)
	}
	return request, token.Plaintext
}

func TestEnrollmentChallengeConsumptionIsSingleUse(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	now := fixture.now
	challengeBytes, err := agentenrollment.GenerateChallenge()
	if err != nil {
		t.Fatalf("generate challenge: %v", err)
	}
	digest := enrollmentDigest(t, "metadata")
	fingerprint := enrollmentFingerprint(t, fixture.key)
	challenge, err := fixture.store.CreateChallenge(t.Context(), EnrollmentChallengeInput{
		GrantID: fixture.grant.ID, ChallengeHash: enrollmentDigestBytes(t, challengeBytes),
		KeyFingerprint: fingerprint, MetadataDigest: digest,
		SourceIP: "10.0.0.5", Now: now, ExpiresAt: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	proof := EnrollmentChallengeProof{ID: challenge.ID, Challenge: challengeBytes,
		KeyFingerprint: fingerprint, MetadataDigest: digest, SourceIP: "10.0.0.5", Now: now}
	proof.SourceIP = "10.0.0.9"
	if _, err := fixture.store.ConsumeChallenge(t.Context(), proof); !errors.Is(err, ErrEnrollmentChallengeUnavailable) {
		t.Fatalf("source mismatch error = %v", err)
	}
	proof.SourceIP = "10.0.0.5"
	if _, err := fixture.store.ConsumeChallenge(t.Context(), proof); err != nil {
		t.Fatalf("consume challenge: %v", err)
	}
	if _, err := fixture.store.ConsumeChallenge(t.Context(), proof); !errors.Is(err, ErrEnrollmentChallengeUnavailable) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestEnrollmentManualApprovalClaimFlow(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	now := fixture.now
	request, token := fixture.createRequest(t, now)
	_ = token
	if request.Status != agentenrollment.RequestPending || request.Metadata.Hostname != "edge-01.example.com" {
		t.Fatalf("request = %#v", request)
	}
	if request.RejectionReason != "" {
		t.Fatalf("unexpected reason %q", request.RejectionReason)
	}

	approved, err := fixture.store.ApproveEnrollmentRequest(t.Context(), EnrollmentApproval{
		RequestID: request.ID, NodeName: "edge-01", GroupID: fixture.grant.DefaultGroupID,
		Tags: []string{"edge"}, Public: true, Host: "10.0.0.5", SSHPort: 22, ActorID: "owner-1"}, now)
	if err != nil {
		t.Fatalf("approve request: %v", err)
	}
	if approved.Status != agentenrollment.RequestApproved || !approved.ReservedNodeID.Valid {
		t.Fatalf("approved = %#v", approved)
	}
	if visible, err := fixture.nodes.ListNodes(t.Context(), ""); err != nil || len(visible) != 0 {
		t.Fatalf("pending node leaked into list: %#v err=%v", visible, err)
	}

	ticket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatalf("generate ticket: %v", err)
	}
	polled, err := fixture.store.RotateEnrollmentTicket(t.Context(), request.ID, ticket, now)
	if err != nil || polled.TicketGeneration != 1 {
		t.Fatalf("rotate ticket: %#v err=%v", polled, err)
	}
	nodeToken, err := agentcredential.Generate()
	if err != nil {
		t.Fatalf("generate node token: %v", err)
	}
	claim, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, nodeToken, now)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.NodeName != "edge-01" || claim.Recovered {
		t.Fatalf("claim = %#v", claim)
	}
	authenticated, err := fixture.nodes.AuthenticateAgentToken(t.Context(), nodeToken.Plaintext)
	if err != nil || authenticated == nil || authenticated.ID != claim.NodeID {
		t.Fatalf("authenticate claimed token: %#v err=%v", authenticated, err)
	}
	if _, err := fixture.store.ClaimEnrollmentRequest(t.Context(),
		EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, nodeToken, now); !errors.Is(err, ErrEnrollmentTicketUnavailable) {
		t.Fatalf("ticket replay error = %v", err)
	}
}

func TestEnrollmentRequestResumeRotatesToken(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	now := fixture.now
	first, _ := fixture.createRequest(t, now)
	second, _ := fixture.createRequest(t, now.Add(time.Minute))
	if first.ID != second.ID {
		t.Fatalf("resume created a duplicate request: %s vs %s", first.ID, second.ID)
	}
	if !second.LastRequest.After(first.LastRequest) || bytes.Equal(second.TokenHash, first.TokenHash) {
		t.Fatal("resume did not rotate the request token")
	}
	page, err := fixture.store.ListEnrollmentRequests(t.Context(), EnrollmentRequestFilter{})
	if err != nil || len(page.Requests) != 1 {
		t.Fatalf("list requests: %#v err=%v", page, err)
	}
	if _, err := fixture.store.RejectEnrollmentRequest(t.Context(),
		EnrollmentRejection{RequestID: first.ID, ActorID: "owner-1", Reason: "not ours"}, now); err != nil {
		t.Fatalf("reject request: %v", err)
	}
	rejected, err := fixture.store.EnrollmentRequest(t.Context(), first.ID)
	if err != nil || rejected.Status != agentenrollment.RequestRejected || rejected.RejectedAt.Time.IsZero() {
		t.Fatalf("rejected = %#v err=%v", rejected, err)
	}
}

func TestEnrollmentAutomaticGrantApprovesImmediately(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	now := fixture.now
	policy := agentenrollment.GrantPolicy{Name: "auto-fleet", Mode: agentenrollment.ApprovalAutomatic,
		MaximumEnrollments: 5, DefaultGroupID: fixture.grant.DefaultGroupID, NodeNamePrefix: "edge",
		SSHPort: 22, UseSourceIPAsHost: true, ExpiresAt: now.Add(24 * time.Hour), Enabled: true}
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	auto, err := fixture.store.CreateGrant(t.Context(), EnrollmentGrantInput{Policy: policy, Secret: secret,
		ActorID: "owner-1"}, now)
	if err != nil {
		t.Fatalf("create automatic grant: %v", err)
	}
	fixture.grant = auto
	request, _ := fixture.createRequest(t, now)
	if request.Status != agentenrollment.RequestApproved || request.NodeNameSnapshot != "edge-edge-01-example-com" {
		t.Fatalf("automatic request = %#v", request)
	}
	reloaded, err := fixture.store.Grant(t.Context(), auto.ID)
	if err != nil || reloaded.ConsumedEnrollments != 1 {
		t.Fatalf("consumed = %#v err=%v", reloaded, err)
	}
}

func TestEnrollmentCleanupExpiresStaleRequests(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	now := fixture.now
	request, _ := fixture.createRequest(t, now)
	later := now.Add(enrollmentRequestLifetime + time.Hour)
	result, err := fixture.store.CleanupEnrollmentState(t.Context(), later)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if result.ExpiredRequests != 1 {
		t.Fatalf("cleanup result = %#v", result)
	}
	expired, err := fixture.store.EnrollmentRequest(t.Context(), request.ID)
	if err != nil || expired.Status != agentenrollment.RequestExpired || expired.RejectionReason != enrollmentReasonRequestExpired {
		t.Fatalf("expired = %#v err=%v", expired, err)
	}
	grants, err := fixture.store.ListGrants(t.Context())
	if err != nil || len(grants) != 1 || grants[0].ConsumedEnrollments != 0 {
		t.Fatalf("capacity restored: %#v err=%v", grants, err)
	}
}

func TestEnrollmentCleanupRemovesRevokedGrantReservations(t *testing.T) {
	fixture := newEnrollmentFixture(t)
	now := fixture.now
	request, _ := fixture.createRequest(t, now)
	if _, err := fixture.store.ApproveEnrollmentRequest(t.Context(), EnrollmentApproval{RequestID: request.ID,
		NodeName: "edge-02", GroupID: fixture.grant.DefaultGroupID, Host: "10.0.0.5", SSHPort: 22,
		ActorID: "owner-1"}, now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := fixture.store.RevokeGrant(t.Context(), fixture.grant.ID, "owner-1", now); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	if _, err := fixture.store.CleanupEnrollmentState(t.Context(), now.Add(time.Minute)); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	var hidden int
	if err := fixture.store.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM nodes WHERE credential_state = 'pending_claim'`).Scan(&hidden); err != nil {
		t.Fatalf("count hidden nodes: %v", err)
	}
	if hidden != 0 {
		t.Fatalf("hidden reservations left behind: %d", hidden)
	}
}

func enrollmentDigest(t *testing.T, payload string) []byte {
	t.Helper()
	digest := agentenrollment.Digest([]byte(payload))
	return digest[:]
}

func enrollmentDigestBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	digest := agentenrollment.Digest(payload)
	return digest[:]
}

func enrollmentFingerprint(t *testing.T, key []byte) []byte {
	t.Helper()
	fingerprint := agentenrollment.Fingerprint(key)
	return fingerprint[:]
}
