package handler

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// protocolCase 保存一次完整入网流程所需的密钥与元数据。
type protocolCase struct {
	publicKey ed25519.PublicKey
	private   ed25519.PrivateKey
	metadata  agentenrollment.Metadata
	canonical []byte
	digest    []byte
}

func newProtocolCase(t *testing.T) protocolCase {
	t.Helper()
	publicKey, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate agent key: %v", err)
	}
	metadata := agentenrollment.Metadata{Hostname: "edge-01.example.com", AdvertisedHost: "10.0.0.5",
		OS: "linux", Architecture: "amd64", AgentVersion: "1.4.0", SSHPort: 22, InstallationNonce: "nonce-1"}
	canonical := metadata.Canonical()
	digest := sha256.Sum256(canonical)
	return protocolCase{publicKey: publicKey, private: private, metadata: metadata, canonical: canonical,
		digest: digest[:]}
}

func (testCase protocolCase) encodedKey() string {
	return base64.RawURLEncoding.EncodeToString(testCase.publicKey)
}

func (testCase protocolCase) encodedDigest() string {
	return base64.RawURLEncoding.EncodeToString(testCase.digest)
}

// postDiscovery 发送一次协议请求，body 已序列化或为 nil。
func postDiscovery(t *testing.T, handler http.HandlerFunc, path, token string, body any, pathID string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		payload = encoded
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if pathID != "" {
		request.SetPathValue("id", pathID)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestAgentEnrollmentProtocolManualFlow(t *testing.T) {
	harness := newDiscoveryHarness(t)
	secret := createDiscoveryGrant(t, harness, agentenrollment.ApprovalManual, 1)
	testCase := newProtocolCase(t)

	challengeRecorder := postDiscovery(t, harness.discovery.HandleChallenge, "/api/v1/agent-enrollment/challenge",
		secret.Plaintext, agentChallengeRequest{PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest()}, "")
	if challengeRecorder.Code != http.StatusCreated {
		t.Fatalf("challenge status = %d body=%s", challengeRecorder.Code, challengeRecorder.Body.String())
	}
	challenge := decodeEnrollmentBody[agentChallengeResponse](t, challengeRecorder)
	challengeBytes, err := base64.RawURLEncoding.DecodeString(challenge.Challenge)
	if err != nil || challenge.Origin != discoveryTestOrigin {
		t.Fatalf("challenge = %#v err=%v", challenge, err)
	}
	expiresAt, ok := parseDiscoveryInstant(challenge.ExpiresAt)
	if !ok {
		t.Fatalf("challenge expiry %q is not RFC3339", challenge.ExpiresAt)
	}
	fingerprint := agentenrollment.Fingerprint(testCase.publicKey)
	binding := agentenrollment.RequestBinding{ChallengeID: challenge.ChallengeID, Challenge: challengeBytes,
		Origin: discoveryTestOrigin, Fingerprint: fingerprint[:], Metadata: testCase.canonical, ExpiresAt: expiresAt}
	createRecorder := postDiscovery(t, harness.discovery.HandleCreateRequest, "/api/v1/agent-enrollment/requests",
		secret.Plaintext, agentRequestCreatePayload{ChallengeID: challenge.ChallengeID,
			Challenge: challenge.Challenge, PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest(), Metadata: metadataPayload(testCase.metadata),
			Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(testCase.private, binding.Canonical()))}, "")
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create request status = %d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	created := decodeEnrollmentBody[agentRequestCreateResponse](t, createRecorder)
	if created.Status != string(agentenrollment.RequestPending) || created.RequestToken == "" {
		t.Fatalf("created request = %#v", created)
	}

	pending := pollDiscovery(t, harness, testCase, created, "")
	if pending.Status != string(agentenrollment.RequestPending) || pending.Ticket != "" {
		t.Fatalf("pending status = %#v", pending)
	}
	if _, err := harness.store.ApproveEnrollmentRequest(t.Context(), store.EnrollmentApproval{
		RequestID: created.RequestID, NodeName: "edge-01", GroupID: harness.groupID,
		Host: "10.0.0.5", SSHPort: 22, ActorID: "owner-1"}, harness.now); err != nil {
		t.Fatalf("approve request: %v", err)
	}
	approved := pollDiscovery(t, harness, testCase, created, "")
	if approved.Status != string(agentenrollment.RequestApproved) || approved.Ticket == "" {
		t.Fatalf("approved status = %#v", approved)
	}
	claimRecorder := claimDiscovery(t, harness, testCase, created, approved.Ticket, "")
	if claimRecorder.Code != http.StatusCreated {
		t.Fatalf("claim status = %d body=%s", claimRecorder.Code, claimRecorder.Body.String())
	}
	claimed := decodeEnrollmentBody[agentClaimResponse](t, claimRecorder)
	if claimed.NodeName != "edge-01" || claimed.ServerURL != discoveryTestOrigin {
		t.Fatalf("claim = %#v", claimed)
	}
	if _, valid := agentcredential.DisplayPrefix(claimed.AgentToken); !valid {
		t.Fatalf("claimed token %q is not an agent credential", claimed.AgentToken)
	}
	replay := claimDiscovery(t, harness, testCase, created, approved.Ticket, "")
	if replay.Code == http.StatusCreated {
		t.Fatal("claim consumed ticket twice")
	}
}

func TestAgentEnrollmentProtocolRejectsBadSignaturesAndTokens(t *testing.T) {
	harness := newDiscoveryHarness(t)
	secret := createDiscoveryGrant(t, harness, agentenrollment.ApprovalManual, 1)
	testCase := newProtocolCase(t)

	recorder := postDiscovery(t, harness.discovery.HandleChallenge, "/api/v1/agent-enrollment/challenge",
		"beat_enroll_v1_not-a-real-secret", agentChallengeRequest{PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest()}, "")
	if recorder.Code == http.StatusCreated {
		t.Fatal("unknown grant produced a challenge")
	}

	challengeRecorder := postDiscovery(t, harness.discovery.HandleChallenge, "/api/v1/agent-enrollment/challenge",
		secret.Plaintext, agentChallengeRequest{PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest()}, "")
	challenge := decodeEnrollmentBody[agentChallengeResponse](t, challengeRecorder)
	forged := agentenrollment.RequestBinding{ChallengeID: challenge.ChallengeID, Challenge: []byte(challenge.Challenge),
		Origin: discoveryTestOrigin, Fingerprint: make([]byte, sha256.Size), Metadata: testCase.canonical,
		ExpiresAt: harness.now.Add(time.Minute)}
	recorder = postDiscovery(t, harness.discovery.HandleCreateRequest, "/api/v1/agent-enrollment/requests",
		secret.Plaintext, agentRequestCreatePayload{ChallengeID: challenge.ChallengeID,
			Challenge: challenge.Challenge, PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest(), Metadata: metadataPayload(testCase.metadata),
			Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(testCase.private, forged.Canonical()))}, "")
	if recorder.Code == http.StatusCreated {
		t.Fatal("forged signature created a request")
	}
	recorder = postDiscovery(t, harness.discovery.HandleRequestStatus,
		"/api/v1/agent-enrollment/requests/unknown/status", "beat_request_v1_bogus",
		agentStatusPayload{Timestamp: formatDiscoveryInstant(harness.now), Nonce: encodedNonce(),
			Signature: base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))}, "unknown")
	if recorder.Code == http.StatusOK {
		t.Fatal("unknown request token polled a status")
	}
}

// pollDiscovery 用请求令牌签名轮询状态。
func pollDiscovery(t *testing.T, harness discoveryHarness, testCase protocolCase,
	created agentRequestCreateResponse, _ string) agentStatusResponse {
	t.Helper()
	binding := agentenrollment.StatusBinding{RequestID: created.RequestID, Origin: discoveryTestOrigin,
		Timestamp: harness.now, Nonce: nonceBytes()}
	recorder := postDiscovery(t, harness.discovery.HandleRequestStatus,
		"/api/v1/agent-enrollment/requests/"+created.RequestID+"/status", created.RequestToken,
		agentStatusPayload{Timestamp: formatDiscoveryInstant(harness.now),
			Nonce:     base64.RawURLEncoding.EncodeToString(binding.Nonce),
			Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(testCase.private, binding.Canonical()))},
		created.RequestID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status poll = %d body=%s", recorder.Code, recorder.Body.String())
	}
	return decodeEnrollmentBody[agentStatusResponse](t, recorder)
}

// claimDiscovery 用票据签名兑换节点凭据。
func claimDiscovery(t *testing.T, harness discoveryHarness, testCase protocolCase,
	created agentRequestCreateResponse, ticket, _ string) *httptest.ResponseRecorder {
	t.Helper()
	nodeID := ""
	if request, err := harness.store.EnrollmentRequest(t.Context(), created.RequestID); err == nil && request.ReservedNodeID.Valid {
		nodeID = request.ReservedNodeID.String
	}
	binding := agentenrollment.ClaimBinding{RequestID: created.RequestID, NodeID: nodeID, Ticket: ticket,
		Origin: discoveryTestOrigin, Timestamp: harness.now, Nonce: nonceBytes()}
	return postDiscovery(t, harness.discovery.HandleClaim,
		"/api/v1/agent-enrollment/requests/"+created.RequestID+"/claim", created.RequestToken,
		agentClaimPayload{Ticket: ticket, Timestamp: formatDiscoveryInstant(harness.now),
			Nonce:     base64.RawURLEncoding.EncodeToString(binding.Nonce),
			Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(testCase.private, binding.Canonical()))},
		created.RequestID)
}

// createDiscoveryGrant 直接用管理端处理器创建一条可用授权。
func createDiscoveryGrant(t *testing.T, harness discoveryHarness, mode agentenrollment.ApprovalMode,
	maximum int) agentenrollment.Secret {
	t.Helper()
	payload := enrollmentGrantUpsertPayload{Name: "discovery-" + string(mode), Mode: string(mode), Enabled: true,
		ExpiresAt: harness.now.Add(24 * time.Hour).Format(time.RFC3339), DefaultGroupID: harness.groupID,
		SSHPort: 22, MaximumEnrollments: maximum, NodeNamePrefix: "edge"}
	recorder := httptest.NewRecorder()
	harness.admin.HandleCreateGrant(recorder, enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/grants", payload))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create grant status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	created := decodeEnrollmentBody[enrollmentGrantSecretResponse](t, recorder)
	secret, err := agentenrollment.DescribeSecret(agentenrollment.GrantSecretPrefix, created.Secret)
	if err != nil {
		t.Fatalf("describe secret: %v", err)
	}
	return secret
}

func metadataPayload(metadata agentenrollment.Metadata) agentMetadataPayload {
	return agentMetadataPayload{Hostname: metadata.Hostname, AdvertisedHost: metadata.AdvertisedHost,
		OS: metadata.OS, Architecture: metadata.Architecture, AgentVersion: metadata.AgentVersion,
		Virtualization: metadata.Virtualization, SSHPort: metadata.SSHPort,
		InstallationNonce: metadata.InstallationNonce}
}

func nonceBytes() []byte {
	return bytes.Repeat([]byte{0x2a}, 16)
}

func encodedNonce() string { return base64.RawURLEncoding.EncodeToString(nonceBytes()) }
