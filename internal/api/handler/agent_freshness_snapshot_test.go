package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
)

func TestRESTAndRealWebSocketShareReceiptFreshnessAndCatalogPrivacy(t *testing.T) {
	fixture := newAgentReportFixture(t)
	fixture.report.CapturedAt = time.Now().UTC().Add(-2 * time.Minute)
	response, ack := fixture.send(t, fixture.report)
	if response.Code != 201 || !ack.MetricsAccepted {
		t.Fatal("fixture report failed")
	}
	if _, err := fixture.sqlite.DB.Exec("UPDATE nodes SET is_public=1 WHERE id=?", fixture.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.sqlite.DB.Exec("UPDATE metric_definitions SET public=0 WHERE metric_key IN ('cpu','net_recv_delta','net_sent_delta')"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+fixture.node.ID, nil)
	request.SetPathValue("id", fixture.node.ID)
	rest := httptest.NewRecorder()
	fixture.handler.HandleGetNode(rest, request)
	if rest.Code != 200 {
		t.Fatal("REST snapshot", rest.Code)
	}
	var body struct {
		Data publicNodeResponse `json:"data"`
	}
	if err := json.Unmarshal(rest.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	metrics := NewMetricsHandler(fixture.handler.nodeStore, fixture.handler.mtsStore).WithMetricCatalog(fixture.handler.metricCatalog)
	startMetricsStream(t, metrics)
	server := httptest.NewServer(http.HandlerFunc(metrics.HandleMetricsWS))
	t.Cleanup(server.Close)
	connection, err := websocket.Dial("ws"+server.URL[4:], "", "http://localhost/")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var ws metricsSnapshot
	if err := websocket.JSON.Receive(connection, &ws); err != nil {
		t.Fatal(err)
	}
	if len(ws.Nodes) != 1 {
		t.Fatal("missing live snapshot")
	}
	assertPrivateStaleSnapshots(t, []publicNodeResponse{body.Data, ws.Nodes[0]}, fixture.report.CapturedAt)
}

func assertPrivateStaleSnapshots(t *testing.T, snapshots []publicNodeResponse, capturedAt time.Time) {
	t.Helper()
	for _, node := range snapshots {
		if node.Status != model.NodeStatusOnline || node.TelemetryState != model.TelemetryStale || node.TelemetryAt == nil || !node.TelemetryAt.Equal(capturedAt) || node.TelemetryAgeSeconds == nil || *node.TelemetryAgeSeconds < 120 {
			t.Fatal("contact and telemetry freshness conflated")
		}
		if _, present := node.Metrics["cpu"]; present || node.Traffic != nil {
			t.Fatal("snapshot disclosed private metric policy")
		}
	}
}
