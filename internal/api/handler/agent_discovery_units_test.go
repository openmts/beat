package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// discoveryUnitHandler 构造只依赖内存状态的协议处理器，用于纯解码与限流分支。
func discoveryUnitHandler(now time.Time) *AgentDiscoveryHandler {
	return &AgentDiscoveryHandler{origin: func(*http.Request) string { return "https://beat.example/" },
		budgets: newDiscoveryBudgets(), now: func() time.Time { return now }, secureOnly: true,
		newChallenge: agentenrollment.GenerateChallenge}
}

func discoveryUnitRequest(target, remote string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, target, nil)
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remote
	return request
}

func TestDiscoveryDecodeRequestRejectsMalformedMaterial(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	challenge := make([]byte, agentenrollment.ChallengeSize)
	metadata := agentMetadataPayload{Hostname: "edge-01", OS: "linux", Architecture: "amd64",
		AgentVersion: "1.2.3", InstallationNonce: "nonce-1"}
	canonical := metadataFromPayload(metadata).Canonical()
	digest := sha256.Sum256(canonical)
	valid := agentRequestCreatePayload{ChallengeID: "challenge-1",
		Challenge:      base64.RawURLEncoding.EncodeToString(challenge),
		PublicKey:      base64.RawURLEncoding.EncodeToString(publicKey),
		Metadata:       metadata,
		Signature:      base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
		MetadataDigest: base64.RawURLEncoding.EncodeToString(digest[:])}
	if _, ok := decodeRequestCreate(valid); !ok {
		t.Fatal("valid discovery material rejected")
	}

	// 每一处材料都会被逐项校验；任何一处畸形都必须整体拒绝。
	cases := map[string]func(*agentRequestCreatePayload){
		"blank challenge id":  func(p *agentRequestCreatePayload) { p.ChallengeID = "   " },
		"short public key":    func(p *agentRequestCreatePayload) { p.PublicKey = "AAAA" },
		"bad challenge":       func(p *agentRequestCreatePayload) { p.Challenge = "***" },
		"bad digest":          func(p *agentRequestCreatePayload) { p.MetadataDigest = encodeDiscoveryBytes(make([]byte, 31)) },
		"bad signature":       func(p *agentRequestCreatePayload) { p.Signature = encodeDiscoveryBytes(make([]byte, 63)) },
		"blank hostname":      func(p *agentRequestCreatePayload) { p.Metadata.Hostname = "" },
		"digest mismatch":     func(p *agentRequestCreatePayload) { p.Metadata.Hostname = "other-host" },
		"bad metadata digest": func(p *agentRequestCreatePayload) { p.MetadataDigest = encodeDiscoveryBytes(digest[:])[:8] },
	}
	for name, mutate := range cases {
		payload := valid
		mutate(&payload)
		if _, ok := decodeRequestCreate(payload); ok {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestDiscoveryMetadataDefaultsSSHPort(t *testing.T) {
	metadata := metadataFromPayload(agentMetadataPayload{Hostname: " edge-01 ", OS: "linux",
		Architecture: "amd64", AgentVersion: "1.0.0", InstallationNonce: "nonce"})
	if metadata.SSHPort != 22 || metadata.Hostname != "edge-01" {
		t.Fatalf("metadata defaults = %+v", metadata)
	}
	explicit := metadataFromPayload(agentMetadataPayload{Hostname: "edge-01", SSHPort: 2222})
	if explicit.SSHPort != 2222 {
		t.Fatalf("explicit ssh port = %d", explicit.SSHPort)
	}
}

func TestDiscoveryClientAddressAndGrantScope(t *testing.T) {
	if _, ok := discoveryClientAddress(discoveryUnitRequest("/x", "not-an-address")); ok {
		t.Fatal("invalid source address accepted")
	}
	if _, ok := discoveryClientAddress(discoveryUnitRequest("/x", "[fe80::1%eth0]:1234")); ok {
		t.Fatal("zoned source address accepted")
	}
	address, ok := discoveryClientAddress(discoveryUnitRequest("/x", "10.0.0.7:1234"))
	if !ok || address.String() != "10.0.0.7" {
		t.Fatalf("address = %v / %v", address, ok)
	}
	grant := &store.EnrollmentGrant{SourceCIDRs: []string{"10.0.0.0/8"}, HostnamePattern: `edge-\d+\.example\.com`}
	if !grantAllowsSource(grant, address) {
		t.Fatal("authorized source rejected")
	}
	if grantAllowsSource(grant, netipMustParse(t, "192.0.2.1")) {
		t.Fatal("unauthorized source accepted")
	}
	if !grantAllowsHostname(grant, "edge-01.example.com") {
		t.Fatal("authorized hostname rejected")
	}
	if grantAllowsHostname(grant, "other.example.com") {
		t.Fatal("unauthorized hostname accepted")
	}
	broken := &store.EnrollmentGrant{HostnamePattern: "("}
	if grantAllowsSource(broken, address) || grantAllowsHostname(broken, "edge-01.example.com") {
		t.Fatal("invalid source policy accepted")
	}
}

func TestDiscoveryCanonicalOriginAndStoreErrorMapping(t *testing.T) {
	handler := discoveryUnitHandler(time.Now())
	request := discoveryUnitRequest("/x", "10.0.0.7:1234")
	if origin := handler.canonicalOrigin(request); origin != "https://beat.example" {
		t.Fatalf("canonical origin = %q", origin)
	}
	handler.origin = nil
	if origin := handler.canonicalOrigin(request); origin != "" {
		t.Fatalf("nil origin = %q", origin)
	}

	cases := []struct {
		name   string
		err    error
		status int
	}{
		{"busy", store.ErrEnrollmentChallengeLimit, http.StatusTooManyRequests},
		{"client", agentenrollment.ErrInvalidMetadata, http.StatusBadRequest},
		{"client signature", store.ErrEnrollmentGrantUnavailable, http.StatusBadRequest},
		{"server", errDiscoveryUnavailable, http.StatusServiceUnavailable},
	}
	for _, testCase := range cases {
		recorder := httptest.NewRecorder()
		handler.respondStoreError(recorder, testCase.err)
		if recorder.Code != testCase.status {
			t.Fatalf("%s status = %d body = %s", testCase.name, recorder.Code, recorder.Body.String())
		}
	}
}

func TestDiscoveryBeginEnforcesTransportAddressAndBudget(t *testing.T) {
	handler := discoveryUnitHandler(time.Now())
	recorder := httptest.NewRecorder()
	if _, ok := handler.begin(recorder, discoveryUnitRequest("/x", "10.0.0.7:1"), handler.budgets.challenge); ok {
		t.Fatal("insecure transport accepted")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("insecure status = %d", recorder.Code)
	}

	insecure := discoveryUnitHandler(time.Now())
	insecure.secureOnly = false
	recorder = httptest.NewRecorder()
	if _, ok := insecure.begin(recorder, discoveryUnitRequest("/x", "bogus"), insecure.budgets.challenge); ok {
		t.Fatal("invalid source address accepted")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid address status = %d", recorder.Code)
	}

	limited := discoveryUnitHandler(time.Now())
	limited.secureOnly = false
	limited.budgets.challenge = newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 1, total: 1, concurrent: 1, keys: 2})
	release, ok := limited.begin(httptest.NewRecorder(), discoveryUnitRequest("/x", "10.0.0.7:1"), limited.budgets.challenge)
	if !ok {
		t.Fatal("first acquisition rejected")
	}
	release()
	limitedRecorder := httptest.NewRecorder()
	if _, ok := limited.begin(limitedRecorder, discoveryUnitRequest("/x", "10.0.0.7:1"), limited.budgets.challenge); ok {
		t.Fatal("over-budget acquisition accepted")
	}
	if limitedRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget status = %d", limitedRecorder.Code)
	}
}

func TestDiscoveryStatusViewAndTicketAvailability(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	handler := discoveryUnitHandler(now)

	rejected := agentStatusView(store.EnrollmentRequest{ID: "request-1",
		Status: agentenrollment.RequestRejected, RejectionReason: "hostname did not match"})
	if rejected.Reason == "" || rejected.NodeID != "" {
		t.Fatalf("rejected view = %+v", rejected)
	}
	reserved := agentStatusView(store.EnrollmentRequest{ID: "request-2",
		Status:         agentenrollment.RequestApproved,
		ReservedNodeID: sql.NullString{String: "node-1", Valid: true},
		ReservedName:   sql.NullString{String: "edge-01", Valid: true}})
	if reserved.NodeID != "node-1" || reserved.NodeName != "edge-01" {
		t.Fatalf("reserved view = %+v", reserved)
	}

	if !handler.ticketAvailable(store.EnrollmentRequest{Status: agentenrollment.RequestApproved}) {
		t.Fatal("approved request denied a ticket")
	}
	claimed := store.EnrollmentRequest{Status: agentenrollment.RequestClaimed,
		RecoveryDeadline: sql.NullTime{Time: now.Add(time.Minute), Valid: true}}
	if !handler.ticketAvailable(claimed) {
		t.Fatal("recoverable claim denied a ticket")
	}
	expired := claimed
	expired.RecoveryDeadline = sql.NullTime{Time: now.Add(-time.Minute), Valid: true}
	if handler.ticketAvailable(expired) {
		t.Fatal("expired recovery window granted a ticket")
	}
	exhausted := claimed
	exhausted.RecoveryAttempts = agentenrollment.MaxClaimRecoveryAttempts
	if handler.ticketAvailable(exhausted) {
		t.Fatal("exhausted recovery attempts granted a ticket")
	}
	if handler.ticketAvailable(store.EnrollmentRequest{Status: agentenrollment.RequestPending}) {
		t.Fatal("pending request granted a ticket")
	}
	if handler.ticketAvailable(store.EnrollmentRequest{Status: agentenrollment.RequestClaimed}) {
		t.Fatal("claim without deadline granted a ticket")
	}
}

func TestDiscoveryStatusSignatureRejectsMalformedProof(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	handler := discoveryUnitHandler(now)
	request := discoveryUnitRequest("/x", "10.0.0.7:1")
	record := store.EnrollmentRequest{ID: "request-1", PublicKey: make([]byte, ed25519.PublicKeySize)}

	proofs := []agentStatusPayload{
		{Timestamp: "not-a-time", Nonce: encodeDiscoveryBytes(make([]byte, 32)),
			Signature: encodeDiscoveryBytes(make([]byte, ed25519.SignatureSize))},
		{Timestamp: formatDiscoveryInstant(now.Add(-time.Hour)), Nonce: encodeDiscoveryBytes(make([]byte, 32)),
			Signature: encodeDiscoveryBytes(make([]byte, ed25519.SignatureSize))},
		{Timestamp: formatDiscoveryInstant(now), Nonce: encodeDiscoveryBytes(make([]byte, 4)),
			Signature: encodeDiscoveryBytes(make([]byte, ed25519.SignatureSize))},
		{Timestamp: formatDiscoveryInstant(now), Nonce: encodeDiscoveryBytes(make([]byte, 32)), Signature: "short"},
	}
	for index, proof := range proofs {
		if handler.verifyStatusSignature(request, &record, proof) {
			t.Fatalf("proof %d accepted", index)
		}
	}

	if !withinDiscoverySkew(now.Add(-time.Minute), now) || !withinDiscoverySkew(now.Add(time.Minute), now) {
		t.Fatal("skew window rejected a valid timestamp")
	}
	if withinDiscoverySkew(now.Add(-time.Hour), now) {
		t.Fatal("skew window accepted a stale timestamp")
	}
	instant, ok := parseDiscoveryInstant(" 2026-10-15T12:00:00Z ")
	if !ok || !instant.Equal(now) {
		t.Fatalf("parsed instant = %v / %v", instant, ok)
	}
	if _, ok := parseDiscoveryInstant("yesterday"); ok {
		t.Fatal("invalid instant accepted")
	}
}

func netipMustParse(t *testing.T, value string) netip.Addr {
	t.Helper()
	address, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatalf("parse address %q: %v", value, err)
	}
	return address
}

var errDiscoveryUnavailable = errors.New("enrollment storage unavailable")

func TestDiscoveryAuthenticateGrantRejectsUnknownSecrets(t *testing.T) {
	harness := newDiscoveryHarness(t)
	handler := harness.discovery

	anonymous := httptest.NewRecorder()
	if _, ok := handler.authenticateGrant(anonymous, discoveryUnitRequest("/x", "10.0.0.7:1")); ok {
		t.Fatal("missing bearer token accepted")
	}
	if anonymous.Code != http.StatusForbidden {
		t.Fatalf("missing bearer status = %d", anonymous.Code)
	}

	wrongPrefix := discoveryUnitRequest("/x", "10.0.0.7:1")
	wrongPrefix.Header.Set("Authorization", "Bearer "+agentenrollment.RequestTokenPrefix+"abc")
	wrongPrefixRecorder := httptest.NewRecorder()
	if _, ok := handler.authenticateGrant(wrongPrefixRecorder, wrongPrefix); ok {
		t.Fatal("grant prefix mismatch accepted")
	}

	unused, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate unused grant: %v", err)
	}
	unknown := discoveryUnitRequest("/x", "10.0.0.7:1")
	unknown.Header.Set("Authorization", "Bearer "+unused.Plaintext)
	unknownRecorder := httptest.NewRecorder()
	if _, ok := handler.authenticateGrant(unknownRecorder, unknown); ok {
		t.Fatal("unknown grant secret accepted")
	}
	if unknownRecorder.Code != http.StatusForbidden {
		t.Fatalf("unknown grant status = %d", unknownRecorder.Code)
	}
}
