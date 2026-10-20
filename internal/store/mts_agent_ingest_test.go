package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func ingestInput(at time.Time, received float64) AgentReportInput {
	return AgentReportInput{Identity: model.AgentIdentity{NodeID: "node", NodeName: "node", Mode: model.AgentCredentialActive, CredentialFingerprint: strings.Repeat("a", 64)}, Now: at,
		Report: model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: at, ReportIntervalMS: 1000,
			Name: "node", Host: "192.0.2.1", Port: 22, Metrics: &model.NodeMetrics{CPU: 25, NetRecvTotal: received, NetSentTotal: received / 2}}}
}

func acceptIngest(t *testing.T, metrics *MTSStore, input AgentReportInput) bool {
	t.Helper()
	attempt, err := metrics.PrepareAgentReport(t.Context(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	duplicate, err := attempt.Commit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return duplicate
}

func TestMTSAgentIngestKeepsNanosecondsAndDuplicateTraffic(t *testing.T) {
	metrics := metricQueryMTS(t)
	at := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	first, second := ingestInput(at.Add(100*time.Nanosecond), 100), ingestInput(at.Add(200*time.Nanosecond), 175)
	if acceptIngest(t, metrics, first) || acceptIngest(t, metrics, second) {
		t.Fatal("new sample was duplicate")
	}
	if !acceptIngest(t, metrics, first) || !acceptIngest(t, metrics, second) {
		t.Fatal("committed retry was not idempotent")
	}
	totals, err := metrics.QueryTrafficUsage(t.Context(), "node", at, at.Add(time.Second))
	if err != nil || totals.Received != 75 || totals.Sent != 37.5 {
		t.Fatalf("duplicate changed traffic: %+v %v", totals, err)
	}
	rows, err := metrics.QueryMetrics(t.Context(), []string{"cpu"}, at, at.Add(time.Second), "node")
	if err != nil || len(rows["cpu"]) != 2 {
		t.Fatalf("nanosecond samples collapsed: %+v %v", rows, err)
	}
	receipt, err := metrics.LatestAgentReceipt(t.Context(), "node")
	if err != nil || receipt == nil || receipt.SampleID != second.Report.SampleID || !receipt.CapturedAt.Equal(second.Report.CapturedAt) {
		t.Fatal("latest receipt was not atomic with sample", err)
	}
}

func TestMTSAgentIngestRejectsConflictOrderingAndCredentialReuse(t *testing.T) {
	metrics := metricQueryMTS(t)
	input := ingestInput(time.Now().UTC(), 100)
	acceptIngest(t, metrics, input)
	for name, change := range map[string]func(*AgentReportInput){
		"changed values":         func(i *AgentReportInput) { i.Report.Metrics = &model.NodeMetrics{CPU: 26} },
		"same ID different time": func(i *AgentReportInput) { i.Report.CapturedAt = i.Report.CapturedAt.Add(time.Nanosecond) },
		"same time different ID": func(i *AgentReportInput) { i.Report.SampleID = uuid.NewString() },
		"different credential":   func(i *AgentReportInput) { i.Identity.CredentialFingerprint = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			retry := input
			change(&retry)
			attempt, err := metrics.PrepareAgentReport(t.Context(), retry, nil)
			if attempt != nil {
				attempt.Close()
			}
			if !errors.Is(err, ErrAgentSampleConflict) {
				t.Fatalf("conflict accepted: %v", err)
			}
		})
	}
	old := ingestInput(input.Now.Add(-time.Second), 90)
	attempt, err := metrics.PrepareAgentReport(t.Context(), old, nil)
	if attempt != nil {
		attempt.Close()
	}
	if !errors.Is(err, ErrAgentSampleOrder) {
		t.Fatalf("out of order accepted: %v", err)
	}
}
