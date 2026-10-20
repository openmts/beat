package main

import (
	"strings"
	"testing"
)

type ingestMetricMonitor struct{ fakeMTSMonitor }

func (ingestMetricMonitor) AgentIngestPrometheus() string {
	return "# TYPE beat_agent_ingest_poisoned gauge\nbeat_agent_ingest_poisoned 1\n"
}

func TestOperabilityExportsAgentIngestHealth(t *testing.T) {
	operations := newTestOperability()
	operations.mts = ingestMetricMonitor{fakeMTSMonitor: fakeMTSMonitor{healthy: false}}
	output := operations.prometheus(t.Context())
	if !strings.Contains(output, "beat_agent_ingest_poisoned 1\n") {
		t.Fatal("agent ingestion metrics are not exposed")
	}
	if !strings.Contains(output, "beat_readiness_check{check=\"mts\"} 0\n") {
		t.Fatal("poisoned ingestion is not reflected in readiness")
	}
}
