package handler

import (
	"crypto/ed25519"
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

const discoveryClaimPath = "/api/v1/agent-enrollment/requests/request-1/claim"

// discoveryRequestRecord 构造一条与请求令牌匹配的落库记录。
func discoveryRequestRecord(testCase protocolCase, token agentenrollment.Secret,
	status agentenrollment.RequestStatus) store.EnrollmentRequest {
	return store.EnrollmentRequest{ID: "request-1", PublicKey: testCase.publicKey, TokenHash: token.Hash,
		Status: status}
}

// discoverySignedStatusPayload 生成一份签名可通过校验的轮询载荷。
func discoverySignedStatusPayload(testCase protocolCase, request store.EnrollmentRequest) agentStatusPayload {
	nonce := make([]byte, discoveryMinimumNonceBytes)
	binding := agentenrollment.StatusBinding{RequestID: request.ID, Origin: discoveryTestOrigin,
		Timestamp: discoveryStubNow, Nonce: nonce}
	return agentStatusPayload{Timestamp: formatDiscoveryInstant(discoveryStubNow),
		Nonce: encodeDiscoveryBytes(nonce), Signature: encodeDiscoveryBytes(ed25519.Sign(testCase.private, binding.Canonical()))}
}

// discoverySignedClaimPayload 生成一份签名可通过校验的认领载荷。
func discoverySignedClaimPayload(testCase protocolCase, request store.EnrollmentRequest,
	ticket agentenrollment.Secret) agentClaimPayload {
	nonce := make([]byte, discoveryMinimumNonceBytes)
	nodeID := ""
	if request.ReservedNodeID.Valid {
		nodeID = request.ReservedNodeID.String
	}
	binding := agentenrollment.ClaimBinding{RequestID: request.ID, NodeID: nodeID, Ticket: ticket.Plaintext,
		Origin: discoveryTestOrigin, Timestamp: discoveryStubNow, Nonce: nonce}
	return agentClaimPayload{Ticket: ticket.Plaintext, Timestamp: formatDiscoveryInstant(discoveryStubNow),
		Nonce: encodeDiscoveryBytes(nonce), Signature: encodeDiscoveryBytes(ed25519.Sign(testCase.private, binding.Canonical()))}
}

// TestAgentDiscoveryHandlerRequestStatusBranches 覆盖轮询路径的认证、验签与票据轮换分支。
func TestAgentDiscoveryHandlerRequestStatusBranches(t *testing.T) {
	testCase := newProtocolCase(t)
	token := discoveryRequestToken(t)
	request := discoveryRequestRecord(testCase, token, agentenrollment.RequestPending)
	valid := discoverySignedStatusPayload(testCase, request)
	staleTimestamp := valid
	staleTimestamp.Timestamp = formatDiscoveryInstant(discoveryStubNow.Add(-time.Hour))
	shortNonce := valid
	shortNonce.Nonce = encodeDiscoveryBytes(make([]byte, discoveryMinimumNonceBytes-1))
	badTimestamp := valid
	badTimestamp.Timestamp = "not-an-instant"
	badSignature := valid
	badSignature.Signature = encodeDiscoveryBytes(make([]byte, ed25519.SignatureSize))
	claimed := func(deadline sql.NullTime, attempts int) store.EnrollmentRequest {
		record := discoveryRequestRecord(testCase, token, agentenrollment.RequestClaimed)
		record.RecoveryDeadline, record.RecoveryAttempts = deadline, attempts
		return record
	}
	approved := discoveryRequestRecord(testCase, token, agentenrollment.RequestApproved)
	live := func() discoveryStubStore { return discoveryStubStore{request: &request} }
	mismatched := discoveryStubStore{request: &store.EnrollmentRequest{ID: "request-1", PublicKey: testCase.publicKey, TokenHash: make([]byte, len(token.Hash))}}

	cases := []struct {
		name string
		stub discoveryStubStore
		auth string
		raw  string
		body any
		want int
	}{
		{"missing bearer", live(), "", "", valid, http.StatusForbidden},
		{"wrong prefix", live(), "beat_grant_v1_abc", "", valid, http.StatusForbidden},
		{"unknown request", discoveryStubStore{requestErr: store.ErrEnrollmentRequestUnavailable}, token.Plaintext, "", valid,
			http.StatusForbidden},
		{"token mismatch", mismatched, token.Plaintext, "", valid, http.StatusForbidden},
		{"malformed json", live(), token.Plaintext, "{", nil, http.StatusBadRequest},
		{"bad timestamp", live(), token.Plaintext, "", badTimestamp, http.StatusForbidden},
		{"stale timestamp", live(), token.Plaintext, "", staleTimestamp, http.StatusForbidden},
		{"short nonce", live(), token.Plaintext, "", shortNonce, http.StatusForbidden},
		{"bad signature", live(), token.Plaintext, "", badSignature, http.StatusForbidden},
		{"pending status", live(), token.Plaintext, "", valid, http.StatusOK},
		{"claimed without deadline", discoveryStubStore{request: pointerTo(claimed(sql.NullTime{}, 0))}, token.Plaintext, "", valid,
			http.StatusOK},
		{"claimed expired", discoveryStubStore{request: pointerTo(claimed(sql.NullTime{
			Time: discoveryStubNow.Add(-time.Minute), Valid: true}, 0))}, token.Plaintext, "", valid, http.StatusOK},
		{"claimed recovery exhausted", discoveryStubStore{request: pointerTo(claimed(sql.NullTime{
			Time: discoveryStubNow.Add(time.Minute), Valid: true}, agentenrollment.MaxClaimRecoveryAttempts))}, token.Plaintext, "", valid,
			http.StatusOK},
		{"approved delivers ticket", discoveryStubStore{request: &approved}, token.Plaintext, "",
			discoverySignedStatusPayload(testCase, approved), http.StatusOK},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			discovery := discoveryStubHandler(test.stub)
			recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleRequestStatus,
				path: discoveryStatusPath, token: test.auth, pathID: "request-1", contentType: "application/json",
				body: encodeDiscoveryCaseBody(t, test.raw, test.body)})
			assertDiscoveryStatus(t, recorder, test.want)
		})
	}
}

// TestAgentDiscoveryHandlerRequestStatusTicketFailures 覆盖票据生成与轮换失败时静默降级。
func TestAgentDiscoveryHandlerRequestStatusTicketFailures(t *testing.T) {
	testCase := newProtocolCase(t)
	token := discoveryRequestToken(t)
	approved := discoveryRequestRecord(testCase, token, agentenrollment.RequestApproved)
	body := encodeDiscoveryCaseBody(t, "", discoverySignedStatusPayload(testCase, approved))
	for name, mutate := range map[string]func(*AgentDiscoveryHandler){
		"generator failure": func(subject *AgentDiscoveryHandler) {
			subject.newTicket = func() (agentenrollment.Secret, error) { return agentenrollment.Secret{}, errors.New("entropy") }
		},
		"rotation failure": func(subject *AgentDiscoveryHandler) {
			subject.store = discoveryStubStore{request: &approved, rotateErr: errors.New("write failed")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			discovery := discoveryStubHandler(discoveryStubStore{request: &approved})
			mutate(discovery)
			recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleRequestStatus,
				path: discoveryStatusPath, token: token.Plaintext, pathID: "request-1", contentType: "application/json", body: body})
			assertDiscoveryStatus(t, recorder, http.StatusOK)
			if rotated := decodeEnrollmentBody[agentStatusResponse](t, recorder); rotated.Ticket != "" {
				t.Fatalf("ticket = %q want empty", rotated.Ticket)
			}
		})
	}
}

// TestAgentDiscoveryHandlerClaimBranches 覆盖认领路径的票据校验与存储错误分支。
func TestAgentDiscoveryHandlerClaimBranches(t *testing.T) {
	testCase := newProtocolCase(t)
	token := discoveryRequestToken(t)
	ticket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatalf("generate ticket: %v", err)
	}
	request := discoveryRequestRecord(testCase, token, agentenrollment.RequestApproved)
	request.ApprovedAt = sql.NullTime{Time: discoveryStubNow.Add(-time.Minute), Valid: true}
	valid := discoverySignedClaimPayload(testCase, request, ticket)
	badTicket := valid
	badTicket.Ticket = "beat_request_v1_abc"
	badTimestamp := valid
	badTimestamp.Timestamp = "not-an-instant"
	shortNonce := valid
	shortNonce.Nonce = encodeDiscoveryBytes(make([]byte, discoveryMinimumNonceBytes-1))
	badSignature := valid
	badSignature.Signature = encodeDiscoveryBytes(make([]byte, ed25519.SignatureSize))
	live := func() discoveryStubStore { return discoveryStubStore{request: &request} }
	metrics := &recordingEnrollmentMetrics{}

	cases := []struct {
		name string
		stub discoveryStubStore
		auth string
		raw  string
		body any
		want int
	}{
		{"missing bearer", live(), "", "", valid, http.StatusForbidden},
		{"malformed json", live(), token.Plaintext, "{", nil, http.StatusBadRequest},
		{"invalid ticket prefix", live(), token.Plaintext, "", badTicket, http.StatusForbidden},
		{"bad timestamp", live(), token.Plaintext, "", badTimestamp, http.StatusForbidden},
		{"short nonce", live(), token.Plaintext, "", shortNonce, http.StatusForbidden},
		{"bad signature", live(), token.Plaintext, "", badSignature, http.StatusForbidden},
		{"claim store failure", discoveryStubStore{request: &request, claimErr: errors.New("write failed")},
			token.Plaintext, "", valid, http.StatusServiceUnavailable},
		{"claimed", discoveryStubStore{request: &request,
			claimResult: store.EnrollmentClaimResult{NodeID: "node-1", NodeName: "edge-01", GroupID: "group-1",
				Host: "10.0.0.5", Port: 22}}, token.Plaintext, "", valid, http.StatusCreated},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			discovery := discoveryStubHandler(test.stub)
			discovery.metrics = metrics
			recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleClaim,
				path: discoveryClaimPath, token: test.auth, pathID: "request-1", contentType: "application/json",
				body: encodeDiscoveryCaseBody(t, test.raw, test.body)})
			assertDiscoveryStatus(t, recorder, test.want)
		})
	}
	if len(metrics.claimLatencies) != 1 {
		t.Fatalf("claim latency samples = %d want 1", len(metrics.claimLatencies))
	}
}

// TestAgentDiscoveryHandlerClaimTokenFailure 覆盖节点令牌生成失败时的固定降级响应。
func TestAgentDiscoveryHandlerClaimTokenFailure(t *testing.T) {
	testCase := newProtocolCase(t)
	token := discoveryRequestToken(t)
	ticket, err := agentenrollment.GenerateClaimTicket()
	if err != nil {
		t.Fatalf("generate ticket: %v", err)
	}
	request := discoveryRequestRecord(testCase, token, agentenrollment.RequestApproved)
	discovery := discoveryStubHandler(discoveryStubStore{request: &request})
	discovery.newNodeToken = func() (agentcredential.Token, error) {
		return agentcredential.Token{}, errors.New("entropy")
	}
	recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleClaim, path: discoveryClaimPath,
		token: token.Plaintext, pathID: "request-1", contentType: "application/json",
		body: encodeDiscoveryCaseBody(t, "", discoverySignedClaimPayload(testCase, request, ticket))})
	assertDiscoveryStatus(t, recorder, http.StatusServiceUnavailable)
}

// TestDiscoveryGrantScopeRejectsMalformedPolicy 覆盖授权策略无法编译时的兜底拒绝。
func TestDiscoveryGrantScopeRejectsMalformedPolicy(t *testing.T) {
	grant := discoveryStubGrant()
	grant.SourceCIDRs = []string{"not-a-cidr"}
	if grantAllowsSource(grant, netip.MustParseAddr("10.0.0.5")) {
		t.Fatal("malformed source CIDR allowed a source")
	}
	grant = discoveryStubGrant()
	grant.HostnamePattern = "(["
	if grantAllowsHostname(grant, "edge-01.example.com") {
		t.Fatal("malformed hostname pattern allowed a host")
	}
}

func pointerTo[T any](value T) *T { return &value }
