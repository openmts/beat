package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openmts/mts"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type agentReportFixture struct {
	handler *NodeHandler
	sqlite  *store.SQLiteStore
	node    *model.Node
	token   string
	report  model.AgentReport
}

func newAgentReportFixture(t *testing.T) agentReportFixture {
	t.Helper()
	sqlite, metrics := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqlite.DB)
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	node, err := nodes.CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "agent", Host: "192.0.2.1", Port: 22}, store.AgentCredential{Hash: credential.Hash, Prefix: credential.Prefix, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return agentReportFixture{handler: NewNodeHandler(nodes, metrics).WithMetricCatalog(store.NewMetricCatalogStore(sqlite.DB)), sqlite: sqlite, node: node, token: credential.Plaintext,
		report: model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: time.Now().UTC(), ReportIntervalMS: 1000,
			Name: "spoofed-name", Host: "192.0.2.99", Port: 2222, Metrics: &model.NodeMetrics{CPU: 25, NetRecvTotal: 100, NetSentTotal: 50}}}
}

func (fixture agentReportFixture) request(t *testing.T, report model.AgentReport) *http.Request {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/report", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+fixture.token)
	return request
}

func (fixture agentReportFixture) send(t *testing.T, report model.AgentReport) (*httptest.ResponseRecorder, model.AgentReportACK) {
	t.Helper()
	response := httptest.NewRecorder()
	middleware.AgentReportAuth(fixture.handler.nodeStore, "", nil)(http.HandlerFunc(fixture.handler.HandleNodeReport)).ServeHTTP(response, fixture.request(t, report))
	var body struct {
		Data model.AgentReportACK `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return response, body.Data
}

func assertAgentReportUnchanged(t *testing.T, fixture agentReportFixture) {
	t.Helper()
	node, err := fixture.handler.nodeStore.GetNode(t.Context(), fixture.node.ID)
	if err != nil || node.Host != fixture.node.Host || !node.LastSeen.Equal(fixture.node.LastSeen) || node.AgentTokenLastUsedAt != nil {
		t.Fatal("rejected report mutated contact/token", err)
	}
	receipt, err := fixture.handler.mtsStore.LatestAgentReceipt(t.Context(), fixture.node.ID)
	if err != nil || receipt != nil {
		t.Fatal("rejected report created receipt", err)
	}
	if fixture.handler.ingestBudget.active != 0 {
		t.Fatal("admission was leaked")
	}
}

func TestAgentReportV2RejectsInvalidEnvelopeBeforeAnyMutation(t *testing.T) {
	for name, change := range map[string]func(*model.AgentReport){
		"old protocol": func(r *model.AgentReport) { r.ProtocolVersion = 1 }, "future": func(r *model.AgentReport) { r.CapturedAt = r.CapturedAt.Add(3 * time.Minute) },
		"expired": func(r *model.AgentReport) { r.CapturedAt = r.CapturedAt.Add(-16 * time.Minute) }, "missing metrics": func(r *model.AgentReport) { r.Metrics = nil },
		"unbound custom": func(r *model.AgentReport) { r.CustomMetrics = map[string]float64{"unknown": 7} }, "bad host": func(r *model.AgentReport) { r.Host = "" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newAgentReportFixture(t)
			report := fixture.report
			change(&report)
			response, ack := fixture.send(t, report)
			if response.Code < 400 || ack.Retryable || ack.ContactAccepted || ack.MetricsAccepted {
				t.Fatal("invalid envelope was accepted", response.Code)
			}
			assertAgentReportUnchanged(t, fixture)
		})
	}
}

func TestAgentReportV2AdmissionIsReadOnlyEvenForCredentialLastUsed(t *testing.T) {
	fixture := newAgentReportFixture(t)
	release, ok := fixture.handler.ingestBudget.acquire(t.Context(), "another", fixture.node.ID)
	if !ok {
		t.Fatal("fixture admission")
	}
	response, ack := fixture.send(t, fixture.report)
	release()
	if response.Code != 429 || !ack.Retryable || ack.ContactAccepted || response.Header().Get("Retry-After") == "" {
		t.Fatal("admission rejection contract", response.Code)
	}
	assertAgentReportUnchanged(t, fixture)
}

func TestAgentReportV2ContactTransactionFailurePreventsNumericWrites(t *testing.T) {
	fixture := newAgentReportFixture(t)
	if _, err := fixture.sqlite.DB.Exec(`CREATE TRIGGER reject_agent_contact BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'contact unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	response, ack := fixture.send(t, fixture.report)
	if response.Code != 503 || ack.ContactAccepted || ack.MetricsAccepted || !ack.Retryable {
		t.Fatal("failed contact was acknowledged")
	}
	assertAgentReportUnchanged(t, fixture)
	if _, err := fixture.sqlite.DB.Exec(`DROP TRIGGER reject_agent_contact`); err != nil {
		t.Fatal(err)
	}
	response, ack = fixture.send(t, fixture.report)
	if response.Code != 201 || !ack.Matches(fixture.report) || !ack.ContactAccepted || !ack.MetricsAccepted {
		t.Fatal("same sample did not recover after contact failure")
	}
}

func TestAgentReportV2ExplicitContactOnlyFailureDoesNotLeakStorageDetails(t *testing.T) {
	fixture := newAgentReportFixture(t)
	if err := fixture.handler.mtsStore.ImportPoints(t.Context(), []mts.Point{{Database: "beat", Measurement: "cpu", Tags: map[string]string{"node": fixture.node.ID},
		Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}, Timestamp: fixture.report.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond}}); err != nil {
		t.Fatal(err)
	}
	response, ack := fixture.send(t, fixture.report)
	if response.Code != 503 || !ack.ContactAccepted || ack.MetricsAccepted || !ack.Retryable || ack.ContactAt == nil {
		t.Fatal("partial acceptance was hidden")
	}
	for _, private := range []string{fixture.token, fixture.node.ID, fixture.report.Host, "schema", "float64"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatal("report disclosed private storage details")
		}
	}
	if healthy, _ := fixture.handler.mtsStore.Health(); healthy {
		t.Fatal("uncertain write left readiness healthy")
	}
}

func TestAgentReportV2RechecksRevokedCredentialAfterAuthentication(t *testing.T) {
	fixture := newAgentReportFixture(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fixture.handler.nodeStore.RevokeAgentToken(t.Context(), fixture.node.ID, time.Now()); err != nil {
			t.Error(err)
			return
		}
		fixture.handler.HandleNodeReport(w, r)
	})
	response := httptest.NewRecorder()
	middleware.AgentReportAuth(fixture.handler.nodeStore, "", nil)(next).ServeHTTP(response, fixture.request(t, fixture.report))
	if response.Code != 401 {
		t.Fatal("revoked authenticated identity reached writes", response.Code)
	}
	assertAgentReportUnchanged(t, fixture)
}
