package agent

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func testReportSample(t *testing.T, metrics model.NodeMetrics, custom map[string]float64) ReportSample {
	t.Helper()
	sample, err := newReportSample(metrics, model.CollectedCustomSamples{Values: custom}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return sample
}

type testACKFixture struct {
	report NodeReport
	key    string
}

func writeTestReportACK(t *testing.T, w http.ResponseWriter, fixture testACKFixture) {
	report, key := fixture.report, fixture.key
	t.Helper()
	now := time.Now().UTC()
	ack := model.AgentReportACK{ProtocolVersion: model.AgentReportProtocol, SampleID: report.SampleID, CapturedAt: report.CapturedAt,
		ContactAccepted: true, MetricsAccepted: true, ContactAt: &now, SSHPublicKey: key}
	if err := json.NewEncoder(w).Encode(struct {
		Data model.AgentReportACK `json:"data"`
	}{ack}); err != nil {
		t.Error(err)
	}
}
