package handler

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/agentenrollment"
)

// seedPendingRequest 走完整协议创建一条待审批请求，供管理端队列用例复用。
func seedPendingRequest(t *testing.T, harness discoveryHarness) (agentRequestCreateResponse, protocolCase) {
	t.Helper()
	secret := createDiscoveryGrant(t, harness, agentenrollment.ApprovalManual, 2)
	testCase := newProtocolCase(t)
	challengeRecorder := postDiscovery(t, harness.discovery.HandleChallenge, "/api/v1/agent-enrollment/challenge",
		secret.Plaintext, agentChallengeRequest{PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest()}, "")
	challenge := decodeEnrollmentBody[agentChallengeResponse](t, challengeRecorder)
	challengeBytes, err := base64.RawURLEncoding.DecodeString(challenge.Challenge)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	expiresAt, _ := parseDiscoveryInstant(challenge.ExpiresAt)
	fingerprint := agentenrollment.Fingerprint(testCase.publicKey)
	binding := agentenrollment.RequestBinding{ChallengeID: challenge.ChallengeID, Challenge: challengeBytes,
		Origin: discoveryTestOrigin, Fingerprint: fingerprint[:], Metadata: testCase.canonical, ExpiresAt: expiresAt}
	recorder := postDiscovery(t, harness.discovery.HandleCreateRequest, "/api/v1/agent-enrollment/requests",
		secret.Plaintext, agentRequestCreatePayload{ChallengeID: challenge.ChallengeID,
			Challenge: challenge.Challenge, PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest(), Metadata: metadataPayload(testCase.metadata),
			Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(testCase.private, binding.Canonical()))}, "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("seed request status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	return decodeEnrollmentBody[agentRequestCreateResponse](t, recorder), testCase
}

func TestAgentEnrollmentRequestQueueListAndDecision(t *testing.T) {
	harness := newDiscoveryHarness(t)
	created, _ := seedPendingRequest(t, harness)

	recorder := httptest.NewRecorder()
	harness.admin.HandleListRequests(recorder, enrollmentAdminRequest(http.MethodGet,
		"/api/v1/admin/agent-enrollment/requests", nil))
	page := decodeEnrollmentBody[enrollmentRequestListResponse](t, recorder)
	if recorder.Code != http.StatusOK || len(page.Requests) != 1 {
		t.Fatalf("list requests = %d %#v", recorder.Code, page)
	}
	listed := page.Requests[0]
	if listed.ID != created.RequestID || listed.Status != string(agentenrollment.RequestPending) {
		t.Fatalf("listed request = %#v", listed)
	}
	if listed.GrantName == "" || listed.GrantID == "" {
		t.Fatalf("list row lost its grant labels: %#v", listed)
	}
	if listed.Hostname != "edge-01.example.com" || listed.ReservedNodeID != "" {
		t.Fatalf("pending row exposed node internals: %#v", listed)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleGetRequest(recorder, withPath(enrollmentAdminRequest(http.MethodGet,
		"/api/v1/admin/agent-enrollment/requests/"+created.RequestID, nil), "id", created.RequestID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get request status = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleListRequests(recorder, enrollmentAdminRequest(http.MethodGet,
		"/api/v1/admin/agent-enrollment/requests?status=nonsense", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown status filter = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	harness.admin.HandleListRequests(recorder, enrollmentAdminRequest(http.MethodGet,
		"/api/v1/admin/agent-enrollment/requests?limit=0", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("zero limit = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleApproveRequest(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/requests/"+created.RequestID+"/approve", enrollmentApprovalPayload{
			NodeName: "edge-07", GroupID: harness.groupID, Public: true, Host: "10.0.0.5", SSHPort: 22}),
		"id", created.RequestID))
	approved := decodeEnrollmentBody[struct {
		Request enrollmentRequestPayload `json:"request"`
	}](t, recorder)
	if recorder.Code != http.StatusOK || approved.Request.Status != string(agentenrollment.RequestApproved) {
		t.Fatalf("approve = %d %#v", recorder.Code, approved)
	}
	if approved.Request.NodeName != "edge-07" || approved.Request.ReservedNodeID == "" {
		t.Fatalf("approved row lost its node: %#v", approved.Request)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleApproveRequest(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/requests/"+created.RequestID+"/approve", enrollmentApprovalPayload{
			NodeName: "edge-08", GroupID: harness.groupID, SSHPort: 22}), "id", created.RequestID))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("double approve status = %d", recorder.Code)
	}
}

func TestAgentEnrollmentRequestRejectionAndValidation(t *testing.T) {
	harness := newDiscoveryHarness(t)
	created, _ := seedPendingRequest(t, harness)

	recorder := httptest.NewRecorder()
	harness.admin.HandleApproveRequest(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/requests/"+created.RequestID+"/approve",
		enrollmentApprovalPayload{NodeName: "  ", GroupID: harness.groupID, SSHPort: 22}),
		"id", created.RequestID))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("blank node name status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleRejectRequest(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/requests/"+created.RequestID+"/reject",
		enrollmentRejectionPayload{Reason: "not ours"}), "id", created.RequestID))
	rejected := decodeEnrollmentBody[struct {
		Request enrollmentRequestPayload `json:"request"`
	}](t, recorder)
	if recorder.Code != http.StatusOK || rejected.Request.Status != string(agentenrollment.RequestRejected) {
		t.Fatalf("reject = %d %#v", recorder.Code, rejected)
	}
	if rejected.Request.Reason != "not ours" || rejected.Request.RejectedBy != "owner-1" {
		t.Fatalf("rejected row lost its audit fields: %#v", rejected.Request)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleRejectRequest(recorder, withPath(enrollmentAdminRequest(http.MethodPost,
		"/api/v1/admin/agent-enrollment/requests/"+created.RequestID+"/reject",
		enrollmentRejectionPayload{Reason: "again"}), "id", created.RequestID))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("double reject status = %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	harness.admin.HandleGetRequest(recorder, withPath(enrollmentAdminRequest(http.MethodGet,
		"/api/v1/admin/agent-enrollment/requests/missing", nil), "id", "missing"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing request status = %d", recorder.Code)
	}
}
