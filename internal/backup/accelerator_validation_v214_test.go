package backup

import (
	"path/filepath"
	"testing"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestValidAcceleratorArchiveRecordRejectsSampleShapes(t *testing.T) {
	definitions := map[string]model.MetricDefinition{
		model.AcceleratorMetricKeys[0]: {Key: model.AcceleratorMetricKeys[0]},
	}
	extraTags := metricRecord{Measurement: model.AcceleratorSampleMeasurement,
		Tags: map[string]string{"node": "node-1", "device": "gpu-0"}, TimestampNS: 1,
		Fields: map[string]mts.FieldValue{}}
	if validAcceleratorArchiveRecord(extraTags, definitions) {
		t.Fatal("accelerator sample with extra tags accepted")
	}
	nodeOnly := metricRecord{Measurement: model.AcceleratorSampleMeasurement,
		Tags: map[string]string{"node": "node-1"}, TimestampNS: 1, Fields: map[string]mts.FieldValue{}}
	if validAcceleratorArchiveRecord(nodeOnly, map[string]model.MetricDefinition{}) {
		t.Fatal("accelerator sample without a builtin definition accepted")
	}
}

func TestValidAcceleratorArchiveDefinitionRequiresVendorTag(t *testing.T) {
	definition := model.MetricDefinition{Source: "builtin", Kind: "gauge", Key: model.AcceleratorMetricKeys[0],
		Unit: "percent", Tags: []string{"node", "device", "location"}}
	if validAcceleratorArchiveDefinition(definition) {
		t.Fatal("accelerator definition without the vendor tag accepted")
	}
}

func TestAcceleratorArchiveMemoryOpenAndFinishFailures(t *testing.T) {
	absent := &acceleratorArchiveMemory{parent: filepath.Join(t.TempDir(), "absent")}
	row := metricRecord{Measurement: "gpu_memory_used_bytes", Tags: map[string]string{}, TimestampNS: 1,
		Fields: map[string]mts.FieldValue{}}
	if err := absent.add(t.Context(), row); err == nil {
		t.Fatal("missing archive workspace accepted")
	}
	if err := absent.add(t.Context(), metricRecord{Measurement: "cpu"}); err != nil {
		t.Fatalf("irrelevant measurement rejected: %v", err)
	}
	directory := t.TempDir()
	database, err := store.NewMTSStore(filepath.Join(directory, "mts"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	pending := &acceleratorArchiveMemory{parent: directory, database: database,
		batch: []mts.Point{{Measurement: "gpu_memory_used_bytes", Timestamp: 1, Precision: mts.PrecisionNanosecond,
			Fields: map[string]mts.FieldValue{"value": mts.Float64Value(1)}}}}
	if err := pending.finish(t.Context()); err == nil {
		t.Fatal("closed archive store accepted pending points")
	}
	empty := &acceleratorArchiveMemory{parent: directory, database: database}
	if err := empty.finish(t.Context()); err == nil {
		t.Fatal("closed archive store accepted state flush")
	}
}
