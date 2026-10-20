//go:build linux

// 该集成用例通过真实服务端 handler 串起 enrollment 与上报流程，
// handler 依赖 Linux-only 的密钥存储，因此只在 Linux 上构建。
package agentinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type enrollmentIntegration struct {
	fixture *fixture
	sqlite  *store.SQLiteStore
	nodes   *store.NodeStore
	mts     *store.MTSStore
	handler http.Handler
}

func newEnrollmentIntegration(t *testing.T) *enrollmentIntegration {
	t.Helper()
	database, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	metrics, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metrics.Close() })
	nodes := store.NewNodeStore(database.DB)
	enrollmentHandler := handler.NewAgentEnrollmentHandler(nodes)
	nodeHandler := handler.NewNodeHandler(nodes, metrics)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/agent-enrollment-tokens", func(w http.ResponseWriter, r *http.Request) {
		principal := model.AdminPrincipal{User: model.AdminUser{ID: "integration-owner", Role: model.AdminRoleOwner}}
		r = r.WithContext(middleware.WithAdminPrincipal(r.Context(), principal))
		enrollmentHandler.HandleIssue(w, r)
	})
	mux.HandleFunc("POST /api/v1/agents/join", enrollmentHandler.HandleJoin)
	mux.Handle("POST /api/v1/nodes/report", middleware.AgentReportAuth(nodes, "", nil)(http.HandlerFunc(nodeHandler.HandleNodeReport)))
	f := newFixture(t)
	f.respond = mux.ServeHTTP
	return &enrollmentIntegration{fixture: f, sqlite: database, nodes: nodes, mts: metrics, handler: mux}
}

func issueIntegrationToken(t *testing.T, integration *enrollmentIntegration) agentjoin.IssueResult {
	t.Helper()
	payload, err := json.Marshal(agentjoin.IssueRequest{ServerURL: integration.fixture.options.ServerURL, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(integration.fixture.options.ServerURL+"/api/v1/admin/agent-enrollment-tokens", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("issue status: %d", response.StatusCode)
	}
	var envelope agentjoin.Envelope[agentjoin.IssueResult]
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != http.StatusCreated || envelope.Data.Command == "" {
		t.Fatal("invalid issuance envelope")
	}
	integration.fixture.options.Token = envelope.Data.Token
	return envelope.Data
}

func TestEnrollmentIntegrationJoinsAndReportsWithSavedConfig(t *testing.T) {
	integration := newEnrollmentIntegration(t)
	issueIntegrationToken(t, integration)
	f := integration.fixture
	summary, err := f.installer.Join(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	config, err := agent.LoadConfig(summary.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.NodeName != "test-node" || config.AdvertisedHost != "127.0.0.1" {
		t.Fatal("negotiated node identity was not persisted")
	}
	reporter, err := agent.NewHTTPReporter(config)
	if err != nil {
		t.Fatal(err)
	}
	first := agent.ReportSample{SampleID: uuid.NewString(), CapturedAt: model.NowUTC(), Metrics: model.NodeMetrics{CPU: 25, Memory: 40}}
	if _, err := reporter.Report(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := reporter.Report(t.Context(), first); err != nil {
		t.Fatalf("retry immutable sample: %v", err)
	}
	node, err := integration.nodes.GetNode(t.Context(), summary.NodeID)
	if err != nil || node == nil || node.Status != model.NodeStatusOnline {
		t.Fatalf("registered node did not become online: %v", err)
	}
	second := agent.ReportSample{SampleID: uuid.NewString(), CapturedAt: model.NowUTC(), Metrics: model.NodeMetrics{CPU: 30, Memory: 45}}
	if _, err := reporter.Report(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	updated, err := integration.nodes.GetNode(t.Context(), summary.NodeID)
	if err != nil || !updated.LastSeen.After(node.LastSeen) {
		t.Fatalf("heartbeat did not advance: %v", err)
	}
	assertIntegrationMetrics(t, integration, summary.NodeID)
	history, err := integration.mts.QueryMetrics(t.Context(), []string{"cpu"}, first.CapturedAt.Add(-time.Second), second.CapturedAt.Add(time.Second), summary.NodeID)
	if err != nil || len(history["cpu"]) != 2 {
		t.Fatalf("immutable sample retry changed history cardinality: %d, %v", len(history["cpu"]), err)
	}
	assertJoinTokenCannotReport(t, integration)
	if _, err := f.installer.Join(t.Context(), f.options); err != nil {
		t.Fatal(err)
	}
	assertSingleIntegrationNode(t, integration, summary.NodeID)
}

func assertJoinTokenCannotReport(t *testing.T, integration *enrollmentIntegration) {
	t.Helper()
	f := integration.fixture
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.options.ServerURL+"/api/v1/nodes/report", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.options.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("join token authorized reporting: %d", response.StatusCode)
	}
}

func TestEnrollmentIntegrationRecoversLostResponseWithFreshToken(t *testing.T) {
	integration := newEnrollmentIntegration(t)
	issued := issueIntegrationToken(t, integration)
	f := integration.fixture
	f.installer.client.Transport = dropFirstJoinResponse{base: http.DefaultTransport}
	if _, err := f.installer.Join(t.Context(), f.options); err == nil {
		t.Fatal("expected lost response")
	}
	nodes, err := integration.nodes.ListNodes(t.Context(), "")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("server did not commit exactly one node: %v", err)
	}
	_, err = integration.sqlite.DB.ExecContext(t.Context(), "UPDATE agent_enrollment_tokens SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Minute), issued.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.installer.client.Transport = http.DefaultTransport
	second := issueIntegrationToken(t, integration)
	if second.Token == issued.Token {
		t.Fatal("issuer repeated a token")
	}
	summary, err := f.installer.Join(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	assertSingleIntegrationNode(t, integration, summary.NodeID)
	if summary.NodeID != nodes[0].ID {
		t.Fatal("fresh token replaced the original node")
	}
	options, err := f.installer.normalizeOptions(f.options)
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.installer.enrollment(options)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := f.installer.exchange(t.Context(), joinExchange{server: f.options.ServerURL, token: f.options.Token, body: state.Request})
	if err != nil || replayed.NodeID != summary.NodeID {
		t.Fatalf("fresh-token replay failed after recovery: %v", err)
	}
}

type dropFirstJoinResponse struct{ base http.RoundTripper }

func (transport dropFirstJoinResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	return nil, errors.New("simulated network failure after server commit")
}

func assertSingleIntegrationNode(t *testing.T, integration *enrollmentIntegration, id string) {
	t.Helper()
	nodes, err := integration.nodes.ListNodes(context.Background(), "")
	if err != nil || len(nodes) != 1 || nodes[0].ID != id {
		t.Fatalf("enrollment created another node: %v", err)
	}
}

func assertIntegrationMetrics(t *testing.T, integration *enrollmentIntegration, id string) {
	t.Helper()
	metrics, err := integration.mts.QueryLatest(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if metrics["cpu"] != 30 || metrics["memory"] != 45 {
		t.Fatal("enrolled node metrics were not stored in MTS")
	}
}
