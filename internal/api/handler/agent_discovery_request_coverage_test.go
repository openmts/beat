package handler

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

const (
	discoveryCreatePath = "/api/v1/agent-enrollment/requests"
	discoveryStatusPath = "/api/v1/agent-enrollment/requests/request-1/status"
)

// discoveryChallengeFixture 返回一条与桩授权匹配的挑战记录。
func discoveryChallengeFixture() store.EnrollmentChallenge {
	return store.EnrollmentChallenge{ID: "challenge-1", GrantID: "grant-1",
		ExpiresAt: discoveryStubNow.Add(time.Minute)}
}

// discoverySignedCreatePayload 生成一份签名可通过校验的建请求载荷。
func discoverySignedCreatePayload(testCase protocolCase, challenge store.EnrollmentChallenge) agentRequestCreatePayload {
	challengeBytes := make([]byte, agentenrollment.ChallengeSize)
	fingerprint := agentenrollment.Fingerprint(testCase.publicKey)
	binding := agentenrollment.RequestBinding{ChallengeID: challenge.ID, Challenge: challengeBytes,
		Origin: discoveryTestOrigin, Fingerprint: fingerprint[:], Metadata: testCase.canonical,
		ExpiresAt: challenge.ExpiresAt}
	return agentRequestCreatePayload{ChallengeID: challenge.ID, Challenge: encodeDiscoveryBytes(challengeBytes),
		PublicKey: testCase.encodedKey(), MetadataDigest: testCase.encodedDigest(),
		Metadata:  metadataPayload(testCase.metadata),
		Signature: encodeDiscoveryBytes(ed25519.Sign(testCase.private, binding.Canonical()))}
}

// TestAgentDiscoveryHandlerCreateRequestBranches 覆盖建请求路径的解码、校验与存储错误分支。
func TestAgentDiscoveryHandlerCreateRequestBranches(t *testing.T) {
	testCase := newProtocolCase(t)
	token := discoveryGrantToken(t)
	challenge := discoveryChallengeFixture()
	valid := discoverySignedCreatePayload(testCase, challenge)
	restricted := discoveryStubGrant()
	restricted.SourceCIDRs = []string{"10.0.0.0/8"}
	live := func() discoveryStubStore {
		return discoveryStubStore{grant: discoveryStubGrant(), consumed: challenge}
	}
	blankChallenge := valid
	blankChallenge.ChallengeID = ""
	shortChallenge := valid
	shortChallenge.Challenge = "short"
	badDigest := valid
	badDigest.MetadataDigest = "not-base64"
	badSignature := valid
	badSignature.Signature = "not-base64"
	badMetadata := valid
	badMetadata.Metadata.Hostname = ""
	digestMismatch := valid
	digestMismatch.Metadata.AdvertisedHost = "10.9.9.9"
	unverified := valid
	unverified.Signature = encodeDiscoveryBytes(make([]byte, ed25519.SignatureSize))
	grantMismatch := live()
	grantMismatch.consumed.GrantID = "grant-2"

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
		{"malformed json", live(), token, "{", nil, http.StatusBadRequest},
		{"blank challenge", live(), token, "", blankChallenge, http.StatusForbidden},
		{"short challenge", live(), token, "", shortChallenge, http.StatusForbidden},
		{"invalid digest", live(), token, "", badDigest, http.StatusForbidden},
		{"invalid signature", live(), token, "", badSignature, http.StatusForbidden},
		{"invalid metadata", live(), token, "", badMetadata, http.StatusForbidden},
		{"digest mismatch", live(), token, "", digestMismatch, http.StatusForbidden},
		{"source outside grant", discoveryStubStore{grant: restricted, consumed: challenge}, token, "", valid,
			http.StatusForbidden},
		{"consume limit", discoveryStubStore{grant: discoveryStubGrant(),
			consumeErr: store.ErrEnrollmentChallengeLimit}, token, "", valid, http.StatusTooManyRequests},
		{"consume unavailable", discoveryStubStore{grant: discoveryStubGrant(),
			consumeErr: store.ErrEnrollmentChallengeUnavailable}, token, "", valid, http.StatusBadRequest},
		{"consume failure", discoveryStubStore{grant: discoveryStubGrant(),
			consumeErr: errors.New("disk offline")}, token, "", valid, http.StatusServiceUnavailable},
		{"grant mismatch", grantMismatch, token, "", valid, http.StatusForbidden},
		{"unverified signature", live(), token, "", unverified, http.StatusForbidden},
		{"create failure", discoveryStubStore{grant: discoveryStubGrant(), consumed: challenge,
			createErr: errors.New("write failed")}, token, "", valid, http.StatusServiceUnavailable},
		{"created", discoveryStubStore{grant: discoveryStubGrant(), consumed: challenge,
			created: store.EnrollmentRequest{ID: "request-1", Status: agentenrollment.RequestPending}}, token, "", valid,
			http.StatusCreated},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			discovery := discoveryStubHandler(test.stub)
			recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleCreateRequest,
				path: discoveryCreatePath, token: test.auth, contentType: "application/json",
				body: encodeDiscoveryCaseBody(t, test.raw, test.body)})
			assertDiscoveryStatus(t, recorder, test.want)
		})
	}
}

// TestAgentDiscoveryHandlerCreateRequestTokenFailure 覆盖令牌生成失败时的固定降级响应。
func TestAgentDiscoveryHandlerCreateRequestTokenFailure(t *testing.T) {
	testCase := newProtocolCase(t)
	challenge := discoveryChallengeFixture()
	discovery := discoveryStubHandler(discoveryStubStore{grant: discoveryStubGrant(), consumed: challenge})
	discovery.newRequestToken = func() (agentenrollment.Secret, error) { return agentenrollment.Secret{}, errors.New("entropy") }
	recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleCreateRequest,
		path: discoveryCreatePath, token: discoveryGrantToken(t), contentType: "application/json",
		body: encodeDiscoveryCaseBody(t, "", discoverySignedCreatePayload(testCase, challenge))})
	assertDiscoveryStatus(t, recorder, http.StatusServiceUnavailable)
}

// TestAgentDiscoveryHandlerCreateRequestResumeMetrics 覆盖恢复既有请求时的计数分支。
func TestAgentDiscoveryHandlerCreateRequestResumeMetrics(t *testing.T) {
	testCase := newProtocolCase(t)
	challenge := discoveryChallengeFixture()
	metrics := &recordingEnrollmentMetrics{}
	discovery := discoveryStubHandler(discoveryStubStore{grant: discoveryStubGrant(), consumed: challenge,
		created: store.EnrollmentRequest{ID: "request-1", Status: agentenrollment.RequestPending}, resumed: true})
	discovery.metrics = metrics
	recorder := postDiscoveryRaw(t, discoveryPostOptions{handler: discovery.HandleCreateRequest,
		path: discoveryCreatePath, token: discoveryGrantToken(t), contentType: "application/json",
		body: encodeDiscoveryCaseBody(t, "", discoverySignedCreatePayload(testCase, challenge))})
	assertDiscoveryStatus(t, recorder, http.StatusCreated)
	if metrics.resumes != 1 {
		t.Fatalf("resume counter = %d want 1", metrics.resumes)
	}
}
