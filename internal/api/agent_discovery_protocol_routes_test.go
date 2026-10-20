package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

const discoveryTestOrigin = "https://example.com"

// 以下结构与 handler 包的响应体保持逐字段一致，测试在这里独立声明以校验线上 JSON。
type discoveryChallengeView struct {
	ChallengeID     string `json:"challenge_id"`
	Challenge       string `json:"challenge"`
	Origin          string `json:"origin"`
	IssuedAt        string `json:"issued_at"`
	ExpiresAt       string `json:"expires_at"`
	ProtocolVersion string `json:"protocol_version"`
}

type discoveryStatusView struct {
	RequestID        string `json:"request_id"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	Ticket           string `json:"ticket"`
	TicketGeneration int    `json:"ticket_generation"`
	TicketExpiresAt  string `json:"ticket_expires_at"`
	NodeID           string `json:"node_id"`
	NodeName         string `json:"node_name"`
}

type discoveryClaimView struct {
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name"`
	GroupID    string `json:"group_id"`
	ServerURL  string `json:"server_url"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	AgentToken string `json:"agent_token"`
	Recovered  bool   `json:"recovered"`
}

type discoveryFixture struct {
	router     *Router
	enrollment *store.AgentEnrollmentStore
	nodes      *store.NodeStore
	groupID    string
	secret     string
	grantID    string
}

func newDiscoveryFixture(t *testing.T, mutate func(*agentenrollment.GrantPolicy)) *discoveryFixture {
	t.Helper()
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	group, err := store.NewGroupStore(sqlite.DB).CreateGroup(context.Background(), "Default")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	enrollment := store.NewAgentEnrollmentStore(sqlite.DB)
	nodes := store.NewNodeStore(sqlite.DB)
	router := NewRouter(nodes, store.NewGroupStore(sqlite.DB), store.NewSSHKeyStore(sqlite.DB),
		store.NewAlertRuleStore(sqlite.DB), store.NewAlertChannelStore(sqlite.DB),
		store.NewAlertEventStore(sqlite.DB), nil, WithAgentEnrollmentStore(enrollment))
	policy := agentenrollment.GrantPolicy{Name: "lab", Mode: agentenrollment.ApprovalManual,
		MaximumEnrollments: 4, DefaultGroupID: group.ID, NodeNamePrefix: "lab", Tags: []string{},
		SSHPort: 22, UseSourceIPAsHost: true, SourceCIDRs: []string{"192.0.2.0/24"},
		HostnamePattern: "^lab-[a-z0-9-]+$", ClaimRecovery: true, Enabled: true, ExpiresAt: time.Now().Add(time.Hour)}
	if mutate != nil {
		mutate(&policy)
	}
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	grant, err := enrollment.CreateGrant(context.Background(),
		store.EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner"}, time.Now())
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	return &discoveryFixture{router: router, enrollment: enrollment, nodes: nodes,
		groupID: group.ID, secret: secret.Plaintext, grantID: grant.ID}
}

// discoveryAgent 模拟目标机器上的 Agent 侧密钥与协议状态。
type discoveryAgent struct {
	publicKey    ed25519.PublicKey
	privateKey   ed25519.PrivateKey
	metadata     agentenrollment.Metadata
	challengeID  string
	challenge    []byte
	expiresAt    time.Time
	requestID    string
	requestToken string
	nodeID       string
	ticket       string
}

func newDiscoveryAgent(t *testing.T, hostname string) *discoveryAgent {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &discoveryAgent{publicKey: publicKey, privateKey: privateKey,
		metadata: agentenrollment.Metadata{Hostname: hostname, OS: "linux", Architecture: "amd64",
			AgentVersion: "1.0.0", SSHPort: 22, InstallationNonce: "install-nonce-1"}}
}

func discoveryPost(t *testing.T, router *Router, path, body, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, discoveryTestOrigin+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}

func discoveryEnvelope[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Code int `json:"code"`
		Data T   `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, response.Body.String())
	}
	return envelope.Data
}

func encodeBinary(value []byte) string { return base64.RawURLEncoding.EncodeToString(value) }

func (agent *discoveryAgent) requestChallenge(t *testing.T, fixture *discoveryFixture) {
	t.Helper()
	digest := agentenrollment.MetadataDigest(agent.metadata)
	body := `{"public_key":"` + encodeBinary(agent.publicKey) + `","metadata_digest":"` + encodeBinary(digest[:]) + `"}`
	response := discoveryPost(t, fixture.router, "/api/v1/agent-enrollment/challenge", body, fixture.secret)
	if response.Code != http.StatusCreated {
		t.Fatalf("challenge status %d: %s", response.Code, response.Body.String())
	}
	payload := discoveryEnvelope[discoveryChallengeView](t, response)
	if payload.Origin != discoveryTestOrigin || payload.ProtocolVersion != agentenrollment.ProtocolVersion {
		t.Fatalf("challenge origin/protocol %q/%q", payload.Origin, payload.ProtocolVersion)
	}
	challenge, err := base64.RawURLEncoding.DecodeString(payload.Challenge)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}
	agent.challengeID, agent.challenge, agent.expiresAt = payload.ChallengeID, challenge, expiresAt.UTC()
}

func (agent *discoveryAgent) createRequest(t *testing.T, fixture *discoveryFixture) *httptest.ResponseRecorder {
	t.Helper()
	canonical := agent.metadata.Canonical()
	digest := agentenrollment.Digest(canonical)
	binding := agentenrollment.RequestBinding{ChallengeID: agent.challengeID, Challenge: agent.challenge,
		Origin: discoveryTestOrigin, Fingerprint: fingerprintOf(agent.publicKey), Metadata: canonical,
		ExpiresAt: agent.expiresAt}
	signature := agentenrollment.SignWith(agent.privateKey, binding.Canonical())
	body := `{"challenge_id":"` + agent.challengeID + `","challenge":"` + encodeBinary(agent.challenge) +
		`","public_key":"` + encodeBinary(agent.publicKey) + `","metadata_digest":"` + encodeBinary(digest[:]) +
		`","metadata":{"hostname":"` + agent.metadata.Hostname + `","os":"` + agent.metadata.OS +
		`","architecture":"` + agent.metadata.Architecture + `","agent_version":"` + agent.metadata.AgentVersion +
		`","ssh_port":22,"installation_nonce":"` + agent.metadata.InstallationNonce + `"},"signature":"` +
		encodeBinary(signature) + `"}`
	return discoveryPost(t, fixture.router, "/api/v1/agent-enrollment/requests", body, fixture.secret)
}

func fingerprintOf(publicKey []byte) []byte {
	sum := agentenrollment.Fingerprint(publicKey)
	return sum[:]
}

func (agent *discoveryAgent) status(t *testing.T, fixture *discoveryFixture) discoveryStatusView {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	nonce := []byte("status-nonce-0001")
	binding := agentenrollment.StatusBinding{RequestID: agent.requestID, Origin: discoveryTestOrigin,
		Timestamp: now, Nonce: nonce}
	signature := agentenrollment.SignWith(agent.privateKey, binding.Canonical())
	body := `{"timestamp":"` + now.Format(time.RFC3339) + `","nonce":"` + encodeBinary(nonce) +
		`","signature":"` + encodeBinary(signature) + `"}`
	path := "/api/v1/agent-enrollment/requests/" + agent.requestID + "/status"
	response := discoveryPost(t, fixture.router, path, body, agent.requestToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status poll %d: %s", response.Code, response.Body.String())
	}
	payload := discoveryEnvelope[discoveryStatusView](t, response)
	if payload.Ticket != "" {
		agent.nodeID, agent.ticket = payload.NodeID, payload.Ticket
	}
	return payload
}

func (agent *discoveryAgent) claim(t *testing.T, fixture *discoveryFixture) *httptest.ResponseRecorder {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	nonce := []byte("claim-nonce-00001")
	binding := agentenrollment.ClaimBinding{RequestID: agent.requestID, NodeID: agent.nodeID,
		Ticket: agent.ticket, Origin: discoveryTestOrigin, Timestamp: now, Nonce: nonce}
	signature := agentenrollment.SignWith(agent.privateKey, binding.Canonical())
	body := `{"ticket":"` + agent.ticket + `","timestamp":"` + now.Format(time.RFC3339) + `","nonce":"` +
		encodeBinary(nonce) + `","signature":"` + encodeBinary(signature) + `"}`
	path := "/api/v1/agent-enrollment/requests/" + agent.requestID + "/claim"
	return discoveryPost(t, fixture.router, path, body, agent.requestToken)
}

type discoveryRequestView struct {
	RequestID    string `json:"request_id"`
	RequestToken string `json:"request_token"`
	Status       string `json:"status"`
	GrantName    string `json:"grant_name"`
}

func discoveryPostFrom(t *testing.T, router *Router, path, body, bearer, remote string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, discoveryTestOrigin+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if remote != "" {
		request.RemoteAddr = remote
	}
	response := httptest.NewRecorder()
	router.ServeHandler().ServeHTTP(response, request)
	return response
}

func discoveryRequestCount(t *testing.T, fixture *discoveryFixture, status agentenrollment.RequestStatus) int {
	t.Helper()
	page, err := fixture.enrollment.ListEnrollmentRequests(context.Background(),
		store.EnrollmentRequestFilter{Status: status})
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	return len(page.Requests)
}

func TestAgentDiscoveryProtocolEnrollsNode(t *testing.T) {
	fixture := newDiscoveryFixture(t, nil)
	agent := newDiscoveryAgent(t, "lab-1")
	agent.requestChallenge(t, fixture)
	created := agent.createRequest(t, fixture)
	if created.Code != http.StatusCreated {
		t.Fatalf("create request %d: %s", created.Code, created.Body.String())
	}
	issued := discoveryEnvelope[discoveryRequestView](t, created)
	if issued.RequestID == "" || !strings.HasPrefix(issued.RequestToken, agentenrollment.RequestTokenPrefix) {
		t.Fatalf("request credentials: %+v", issued)
	}
	agent.requestID, agent.requestToken = issued.RequestID, issued.RequestToken
	if pending := agent.status(t, fixture); pending.Status != string(agentenrollment.RequestPending) || pending.Ticket != "" {
		t.Fatalf("pending status: %+v", pending)
	}
	if _, err := fixture.enrollment.ApproveEnrollmentRequest(context.Background(), store.EnrollmentApproval{
		RequestID: agent.requestID, NodeName: "lab-1", GroupID: fixture.groupID, SSHPort: 22,
		ActorID: "owner"}, time.Now()); err != nil {
		t.Fatalf("approve: %v", err)
	}
	nodes, err := fixture.nodes.ListNodes(context.Background(), "")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("pending claim node leaked into node list: %d", len(nodes))
	}
	approved := agent.status(t, fixture)
	if approved.Status != string(agentenrollment.RequestApproved) || approved.Ticket == "" || approved.NodeID == "" {
		t.Fatalf("approved status: %+v", approved)
	}
	claimed := agent.claim(t, fixture)
	if claimed.Code != http.StatusCreated {
		t.Fatalf("claim %d: %s", claimed.Code, claimed.Body.String())
	}
	result := discoveryEnvelope[discoveryClaimView](t, claimed)
	if !strings.HasPrefix(result.AgentToken, "beat_agent_v1_") || result.Recovered || result.ServerURL != discoveryTestOrigin {
		t.Fatalf("claim result: %+v", result)
	}
	if count := discoveryRequestCount(t, fixture, agentenrollment.RequestClaimed); count != 1 {
		t.Fatalf("claimed request count %d", count)
	}
	if retry := agent.claim(t, fixture); retry.Code == http.StatusCreated {
		t.Fatal("claim ticket was reusable")
	}
}

func TestAgentDiscoveryProtocolRejectsUnapprovedInputs(t *testing.T) {
	fixture := newDiscoveryFixture(t, nil)
	agent := newDiscoveryAgent(t, "lab-1")
	digest := agentenrollment.MetadataDigest(agent.metadata)
	body := `{"public_key":"` + encodeBinary(agent.publicKey) + `","metadata_digest":"` + encodeBinary(digest[:]) + `"}`
	unknown := discoveryPost(t, fixture.router, "/api/v1/agent-enrollment/challenge", body, fixture.secret+"x")
	if unknown.Code != http.StatusForbidden {
		t.Fatalf("unknown grant secret status %d", unknown.Code)
	}
	outside := discoveryPostFrom(t, fixture.router, "/api/v1/agent-enrollment/challenge", body,
		fixture.secret, "198.51.100.7:1234")
	if outside.Code != http.StatusForbidden {
		t.Fatalf("source outside grant CIDR status %d", outside.Code)
	}
	agent.requestChallenge(t, fixture)
	tampered := strings.Replace(encodeBinary(agentenrollment.SignWith(agent.privateKey, []byte("nope"))),
		"a", "b", 1)
	response := discoveryPostFrom(t, fixture.router, "/api/v1/agent-enrollment/requests",
		tamperedCreateBody(agent, tampered), fixture.secret, "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("tampered signature status %d", response.Code)
	}
}

// tamperedCreateBody 生成一份字段合法但签名不符的建请求体。
func tamperedCreateBody(agent *discoveryAgent, signature string) string {
	canonical := agent.metadata.Canonical()
	digest := agentenrollment.Digest(canonical)
	return `{"challenge_id":"` + agent.challengeID + `","challenge":"` + encodeBinary(agent.challenge) +
		`","public_key":"` + encodeBinary(agent.publicKey) + `","metadata_digest":"` + encodeBinary(digest[:]) +
		`","metadata":{"hostname":"` + agent.metadata.Hostname + `","os":"` + agent.metadata.OS +
		`","architecture":"` + agent.metadata.Architecture + `","agent_version":"` + agent.metadata.AgentVersion +
		`","ssh_port":22,"installation_nonce":"` + agent.metadata.InstallationNonce + `"},"signature":"` +
		signature + `"}`
}
