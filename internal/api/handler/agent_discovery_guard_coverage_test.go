package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

const discoveryChallengePath = "/api/v1/agent-enrollment/challenge"

// TestAgentDiscoveryHandlerTransportGuards 覆盖传输安全、来源地址与规范来源的守卫分支。
func TestAgentDiscoveryHandlerTransportGuards(t *testing.T) {
	secureOnly := NewAgentDiscoveryHandler(discoveryStubStore{}, nil, true)
	secureOnly.now = func() time.Time { return discoveryStubNow }
	assertDiscoveryStatus(t, postDiscoveryRaw(t, discoveryPostOptions{handler: secureOnly.HandleChallenge,
		path: discoveryChallengePath, contentType: "application/json", body: "{}"}), http.StatusBadRequest)
	request := httptest.NewRequest(http.MethodPost, discoveryChallengePath, nil)
	if origin := secureOnly.canonicalOrigin(request); origin != "" {
		t.Fatalf("origin = %q want empty", origin)
	}

	discovery := discoveryStubHandler(discoveryStubStore{grant: discoveryStubGrant()})
	for _, remoteAddr := range []string{"not-an-address", "[fe80::1%25eth0]:1234"} {
		recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleChallenge,
			path: discoveryChallengePath, contentType: "application/json", body: "{}", remoteAddr: remoteAddr})
		assertDiscoveryStatus(t, recorder, http.StatusBadRequest)
	}
}

// TestAgentDiscoveryHandlerBudgetGuard 覆盖每 IP 配额耗尽后的限流响应。
func TestAgentDiscoveryHandlerBudgetGuard(t *testing.T) {
	testCase := newProtocolCase(t)
	discovery := discoveryStubHandler(discoveryStubStore{grant: discoveryStubGrant(),
		challenge: store.EnrollmentChallenge{ID: "challenge-1"}})
	discovery.budgets.challenge = newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 1, total: 4, concurrent: 4, keys: 4})
	options := discoveryPostOptions{handler: discovery.HandleChallenge, path: discoveryChallengePath,
		token: discoveryGrantToken(t), contentType: "application/json",
		body: encodeDiscoveryCaseBody(t, "", agentChallengeRequest{PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest()})}
	assertDiscoveryStatus(t, postDiscoveryRaw(t, options), http.StatusCreated)
	assertDiscoveryStatus(t, postDiscoveryRaw(t, options), http.StatusTooManyRequests)
}

// TestAgentDiscoveryHandlerChallengeFailureInjection 覆盖挑战生成失败时的固定降级响应。
func TestAgentDiscoveryHandlerChallengeFailureInjection(t *testing.T) {
	testCase := newProtocolCase(t)
	discovery := discoveryStubHandler(discoveryStubStore{grant: discoveryStubGrant()})
	discovery.newChallenge = func() ([]byte, error) { return nil, errors.New("entropy unavailable") }
	recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleChallenge,
		path: discoveryChallengePath, token: discoveryGrantToken(t), contentType: "application/json",
		body: encodeDiscoveryCaseBody(t, "", agentChallengeRequest{PublicKey: testCase.encodedKey(),
			MetadataDigest: testCase.encodedDigest()})})
	assertDiscoveryStatus(t, recorder, http.StatusServiceUnavailable)
}

// TestAgentDiscoveryHandlerChallengeRejections 覆盖申请挑战时的拒绝与存储错误映射。
func TestAgentDiscoveryHandlerChallengeRejections(t *testing.T) {
	testCase := newProtocolCase(t)
	token := discoveryGrantToken(t)
	valid := agentChallengeRequest{PublicKey: testCase.encodedKey(), MetadataDigest: testCase.encodedDigest()}
	restricted := discoveryStubGrant()
	restricted.SourceCIDRs = []string{"10.0.0.0/8"}
	live := func() discoveryStubStore { return discoveryStubStore{grant: discoveryStubGrant()} }

	cases := []struct {
		name        string
		stub        discoveryStubStore
		auth        string
		contentType string
		raw         string
		body        any
		want        int
	}{
		{"missing bearer", live(), "", "application/json", "", valid, http.StatusForbidden},
		{"wrong token prefix", live(), "beat_request_v1_abc", "application/json", "", valid, http.StatusForbidden},
		{"unknown grant", discoveryStubStore{grantErr: store.ErrEnrollmentGrantUnavailable}, token, "application/json", "", valid, http.StatusForbidden},
		{"unparsable grant list", discoveryStubStore{grant: restricted}, token, "application/json", "", valid, http.StatusForbidden},
		{"unsupported media", live(), token, "text/plain", "", valid, http.StatusUnsupportedMediaType},
		{"malformed json", live(), token, "application/json", "{", nil, http.StatusBadRequest},
		{"trailing json", live(), token, "application/json", `{"public_key":"` + testCase.encodedKey() + `"} {"x":1}`, nil, http.StatusBadRequest},
		{"unknown field", live(), token, "application/json", `{"public_key":"x","metadata_digest":"y","extra":1}`, nil, http.StatusBadRequest},
		{"oversized body", live(), token, "application/json", `{"public_key":"` + strings.Repeat("a", agentDiscoveryMaxBodyBytes+64) + `"}`, nil,
			http.StatusRequestEntityTooLarge},
		{"invalid public key", live(), token, "application/json", "",
			agentChallengeRequest{PublicKey: "not-base64", MetadataDigest: testCase.encodedDigest()}, http.StatusForbidden},
		{"invalid digest", live(), token, "application/json", "",
			agentChallengeRequest{PublicKey: testCase.encodedKey(), MetadataDigest: "not-base64"}, http.StatusForbidden},
		{"challenge limit", discoveryStubStore{grant: discoveryStubGrant(),
			challengeErr: store.ErrEnrollmentChallengeLimit}, token, "application/json", "", valid, http.StatusTooManyRequests},
		{"challenge unavailable", discoveryStubStore{grant: discoveryStubGrant(),
			challengeErr: store.ErrEnrollmentChallengeUnavailable}, token, "application/json", "", valid, http.StatusBadRequest},
		{"challenge store failure", discoveryStubStore{grant: discoveryStubGrant(),
			challengeErr: errors.New("disk offline")}, token, "application/json", "", valid, http.StatusServiceUnavailable},
		{"issued challenge", discoveryStubStore{grant: discoveryStubGrant(),
			challenge: store.EnrollmentChallenge{ID: "challenge-1"}}, token, "application/json", "", valid, http.StatusCreated},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			discovery := discoveryStubHandler(test.stub)
			recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleChallenge,
				path: discoveryChallengePath, token: test.auth, contentType: test.contentType,
				body: encodeDiscoveryCaseBody(t, test.raw, test.body)})
			assertDiscoveryStatus(t, recorder, test.want)
		})
	}
}
