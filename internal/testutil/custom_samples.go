package testutil

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func CustomSourceFixture(at time.Time, metric model.CustomMetric) model.CustomSampleMetadata {
	return model.CustomSampleMetadata{SampleID: uuid.NewString(), CapturedAt: at, CadenceMS: int64(metric.IntervalSeconds) * 1000, DefinitionVersion: model.CustomMetricDefinitionVersion(metric)}
}

func VersionedCustomAgentReport(t *testing.T, body string, definitions []model.CustomMetric) string {
	t.Helper()
	var report model.AgentReport
	if err := json.Unmarshal([]byte(VersionedAgentReport(t, body)), &report); err != nil {
		t.Fatal(err)
	}
	report.CustomSamples = make(map[string]model.CustomSampleMetadata)
	for _, definition := range definitions {
		if _, ok := report.CustomMetrics[definition.Name]; ok {
			report.CustomSamples[definition.Name] = CustomSourceFixture(report.CapturedAt, definition)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
