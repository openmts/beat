package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func customSourceReportFixture(t *testing.T) (agentReportFixture, model.CustomMetric) {
	t.Helper()
	fixture := newAgentReportFixture(t)
	metric, err := store.NewCustomMetricStore(fixture.sqlite.DB).Create(t.Context(), model.CustomMetric{Name: "queue", ShellCommand: "printf 7", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewCustomMetricStore(fixture.sqlite.DB).SetMetricNodes(t.Context(), metric.ID, []string{fixture.node.ID}); err != nil {
		t.Fatal(err)
	}
	fixture.report.CustomMetrics = map[string]float64{"queue": 7}
	fixture.report.CustomSamples = map[string]model.CustomSampleMetadata{"queue": testutil.CustomSourceFixture(fixture.report.CapturedAt.Add(-10*time.Second), *metric)}
	return fixture, *metric
}

func TestCustomSourceSurvivesHTTPMTSAndUnrelatedHostReceipts(t *testing.T) {
	fixture, _ := customSourceReportFixture(t)
	response, _ := fixture.send(t, fixture.report)
	if response.Code != 201 {
		t.Fatal("custom source admission", response.Code)
	}
	first, err := fixture.handler.mtsStore.QueryCustomSample(t.Context(), fixture.node.ID, "custom_queue")
	if err != nil || first == nil || first.Value != 7 || first.Source != fixture.report.CustomSamples["queue"] {
		t.Fatal("actual custom source lost", err)
	}
	host := fixture.report
	host.SampleID = uuid.NewString()
	host.CapturedAt = host.CapturedAt.Add(time.Second)
	host.CustomMetrics = nil
	host.CustomSamples = nil
	response, _ = fixture.send(t, host)
	if response.Code != 201 {
		t.Fatal(response.Code)
	}
	latest, err := fixture.handler.mtsStore.QueryCustomSample(t.Context(), fixture.node.ID, "custom_queue")
	if err != nil || latest.Source != first.Source || !latest.ReportAt.Equal(first.ReportAt) {
		t.Fatal("host receipt refreshed old custom evidence", err)
	}
}

func TestCustomSourceCannotSpoofCadenceOrExecutedDefinition(t *testing.T) {
	for _, scenario := range []string{"cadence", "old definition"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, definition := customSourceReportFixture(t)
			if scenario == "cadence" {
				source := fixture.report.CustomSamples["queue"]
				source.CadenceMS = 3600000
				fixture.report.CustomSamples["queue"] = source
			} else {
				definition.ShellCommand = "printf 8"
				if _, err := store.NewCustomMetricStore(fixture.sqlite.DB).UpdateWithRegistration(t.Context(), definition.ID, definition); err != nil {
					t.Fatal(err)
				}
			}
			response, ack := fixture.send(t, fixture.report)
			if response.Code != 409 || ack.ErrorCode != "custom_source_changed" || ack.Retryable || ack.ContactAccepted || ack.MetricsAccepted {
				t.Fatal("spoofed source metadata admitted", response.Code)
			}
			assertAgentReportUnchanged(t, fixture)
		})
	}
}
