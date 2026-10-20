package store

import (
	"errors"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// TestMTSCatalogAttachmentRejectsIncompatibleSpecialMeasurements 覆盖特殊测量
// （加速卡采样与 Agent 回执）的字段形状门禁：形状不符时必须拒绝挂载，
// 而不是把半截历史恢复成正常指标。
func TestMTSCatalogAttachmentRejectsIncompatibleSpecialMeasurements(t *testing.T) {
	scenarios := []struct {
		name        string
		measurement string
		fields      map[string]mts.FieldValue
	}{
		{"accelerator subset", model.AcceleratorSampleMeasurement, map[string]mts.FieldValue{"value": mts.Int64Value(1)}},
		{"accelerator wrong types", model.AcceleratorSampleMeasurement, map[string]mts.FieldValue{
			"sample_id": mts.Int64Value(1), "payload_digest": mts.StringValue("digest"),
			"cadence_ms": mts.Int64Value(1000), "inventory": mts.StringValue("{}")}},
		{"receipt subset", model.AgentReceiptMeasurement, map[string]mts.FieldValue{"sample_id": mts.StringValue("sample")}},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			sqlite := setupTestDB(t)
			metrics := metricQueryMTS(t)
			point := mts.Point{Measurement: scenario.measurement, Tags: map[string]string{"node": "node"},
				Fields: scenario.fields, Timestamp: time.Now().UTC().UnixNano()}
			if err := metrics.ImportPoints(t.Context(), []mts.Point{point}); err != nil {
				t.Fatal(err)
			}
			err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB))
			if !errors.Is(err, ErrAgentReceiptInvalid) {
				t.Fatalf("mismatched %s schema = %v", scenario.measurement, err)
			}
		})
	}
}

func TestMTSCatalogAttachmentFailsClosedOnBrokenCatalog(t *testing.T) {
	metrics := metricQueryMTS(t)
	if err := metrics.WriteMetric(t.Context(), "node", "cpu", 1, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	closed := closedSQLiteStore(t)
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(closed.DB)); err == nil {
		t.Fatal("closed catalog accepted stored inventory")
	}
}
