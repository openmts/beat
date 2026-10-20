package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func TestAgentReportLostHTTPResponseRetriesDurableSample(t *testing.T) {
	fixture := newAgentReportFixture(t)
	handler := middleware.AgentReportAuth(fixture.handler.nodeStore, "", nil)(http.HandlerFunc(fixture.handler.HandleNodeReport))
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			committed := httptest.NewRecorder()
			handler.ServeHTTP(committed, r)
			if committed.Code != 201 {
				t.Error("first request did not commit", committed.Code)
			}
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	reporter, err := agent.NewHTTPReporter(agent.Config{ServerURL: server.URL, AgentToken: fixture.token, NodeName: fixture.node.Name, AdvertisedHost: fixture.report.Host, SSHPort: fixture.report.Port, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	sample := agent.ReportSample{SampleID: fixture.report.SampleID, CapturedAt: fixture.report.CapturedAt, Metrics: *fixture.report.Metrics}
	if _, err := reporter.Report(t.Context(), sample); err == nil {
		t.Fatal("lost response appeared acknowledged")
	}
	if _, err := reporter.Report(t.Context(), sample); err != nil {
		t.Fatal("durable duplicate did not ACK", err)
	}
	values, err := fixture.handler.mtsStore.QueryMetrics(t.Context(), []string{"cpu"}, sample.CapturedAt.Add(-time.Second), sample.CapturedAt.Add(time.Second), fixture.node.ID)
	if err != nil || len(values["cpu"]) != 1 {
		t.Fatal("response loss created duplicate numeric sample", err)
	}
}

func TestAgentReportOlderDuplicateCannotRewindInventoryOrDoubleTraffic(t *testing.T) {
	fixture := newAgentReportFixture(t)
	response, _ := fixture.send(t, fixture.report)
	if response.Code != 201 {
		t.Fatal(response.Code)
	}
	newer := fixture.report
	newer.SampleID = uuid.NewString()
	newer.CapturedAt = newer.CapturedAt.Add(time.Second)
	newer.Host = "192.0.2.100"
	newer.System = model.SystemInfo{AgentVersion: "newer-version", CPUModel: "current-model"}
	newer.Metrics = &model.NodeMetrics{NetRecvTotal: 160, NetSentTotal: 90}
	response, _ = fixture.send(t, newer)
	if response.Code != 201 {
		t.Fatal(response.Code)
	}
	response, ack := fixture.send(t, fixture.report)
	if response.Code != 200 || !ack.Duplicate || !ack.Matches(fixture.report) {
		t.Fatal("older committed sample did not ACK")
	}
	node, err := fixture.handler.nodeStore.GetNode(t.Context(), fixture.node.ID)
	if err != nil || node.Host != newer.Host || node.AgentVersion != newer.System.AgentVersion || node.CPUModel != newer.System.CPUModel {
		t.Fatal("duplicate rewound inventory", err)
	}
	totals, err := fixture.handler.mtsStore.QueryTrafficUsage(t.Context(), node.ID, fixture.report.CapturedAt.Add(-time.Second), newer.CapturedAt.Add(time.Second))
	if err != nil || totals.Received != 60 || totals.Sent != 40 {
		t.Fatal("duplicate changed traffic", totals, err)
	}
}

func TestAgentReportSnapshotWaitCancelsBeforeContactMutation(t *testing.T) {
	fixture := newAgentReportFixture(t)
	snapshot, err := fixture.handler.mtsStore.BeginMetricSnapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	request := fixture.request(t, fixture.report).WithContext(ctx)
	response := httptest.NewRecorder()
	middleware.AgentReportAuth(fixture.handler.nodeStore, "", nil)(http.HandlerFunc(fixture.handler.HandleNodeReport)).ServeHTTP(response, request)
	cancel()
	snapshot.Close()
	if response.Code != 503 {
		t.Fatal("frozen report status", response.Code)
	}
	assertAgentReportUnchanged(t, fixture)
}

type failedACKWriter struct {
	header http.Header
	status int
}

func (writer *failedACKWriter) Header() http.Header { return writer.header }

func (writer *failedACKWriter) WriteHeader(status int) { writer.status = status }

func (*failedACKWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection lost during encoding")
}

func TestAgentReportEncodingFailureStillResolvesByCommittedReceipt(t *testing.T) {
	fixture := newAgentReportFixture(t)
	writer := &failedACKWriter{header: make(http.Header)}
	middleware.AgentReportAuth(fixture.handler.nodeStore, "", nil)(http.HandlerFunc(fixture.handler.HandleNodeReport)).ServeHTTP(writer, fixture.request(t, fixture.report))
	if writer.status != 201 {
		t.Fatal("first request did not reach response encoding")
	}
	response, ack := fixture.send(t, fixture.report)
	if response.Code != 200 || !ack.Duplicate || !ack.MetricsAccepted {
		t.Fatal("encoding failure was not idempotent")
	}
}
