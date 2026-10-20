package backup

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func acceleratorArchiveDefinitions() map[string]model.MetricDefinition {
	units := []string{"percent", "bytes", "bytes", "celsius", "watts", "watts"}
	definitions := map[string]model.MetricDefinition{}
	for index, key := range model.AcceleratorMetricKeys {
		definitions[key] = model.MetricDefinition{Key: key, Source: "builtin", Kind: "gauge", Unit: units[index], Tags: []string{"node", "device", "vendor"}}
	}
	return definitions
}

func acceleratorArchiveValue(key string, value float64) metricRecord {
	return metricRecord{Measurement: key, TimestampNS: time.Now().Add(-time.Minute).UnixNano(), Tags: map[string]string{"node": "node", "device": strings.Repeat("a", 32), "vendor": "nvidia"}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(value)}}
}

func TestAcceleratorArchiveScalarSchema(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*metricRecord, map[string]model.MetricDefinition)
	}{
		{"unknown vendor", func(r *metricRecord, _ map[string]model.MetricDefinition) { r.Tags["vendor"] = "untrusted" }},
		{"raw identity", func(r *metricRecord, _ map[string]model.MetricDefinition) { r.Tags["device"] = "GPU-serial-number" }},
		{"extra tag", func(r *metricRecord, _ map[string]model.MetricDefinition) { r.Tags["model"] = "model" }},
		{"missing tag", func(r *metricRecord, _ map[string]model.MetricDefinition) { delete(r.Tags, "vendor") }},
		{"wrong field", func(r *metricRecord, _ map[string]model.MetricDefinition) { r.Fields["value"] = mts.Int64Value(3) }},
		{"NaN", func(r *metricRecord, _ map[string]model.MetricDefinition) {
			r.Fields["value"] = mts.Float64Value(math.NaN())
		}},
		{"fractional bytes", func(r *metricRecord, _ map[string]model.MetricDefinition) { r.Fields["value"] = mts.Float64Value(1.5) }},
		{"wrong unit", func(r *metricRecord, d map[string]model.MetricDefinition) {
			v := d[r.Measurement]
			v.Unit = "percent"
			d[r.Measurement] = v
		}},
		{"wrong tags", func(r *metricRecord, d map[string]model.MetricDefinition) {
			v := d[r.Measurement]
			v.Tags = []string{"node"}
			d[r.Measurement] = v
		}},
		{"missing catalog", func(r *metricRecord, d map[string]model.MetricDefinition) { delete(d, r.Measurement) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			row, definitions := acceleratorArchiveValue("gpu_memory_used_bytes", 3), acceleratorArchiveDefinitions()
			if !validCatalogMetricRecord(row, math.MaxInt64, definitions) {
				t.Fatal("valid GPU row rejected")
			}
			test.mutate(&row, definitions)
			if validCatalogMetricRecord(row, math.MaxInt64, definitions) {
				t.Fatal("invalid GPU row accepted")
			}
		})
	}
	if validMetricRecord(acceleratorArchiveValue("gpu_memory_used_bytes", 3), math.MaxInt64) {
		t.Fatal("legacy format accepted undeclared GPU measurement")
	}
}

func TestAcceleratorArchiveSampleMetadata(t *testing.T) {
	report := model.AcceleratorReport{Version: 1, CollectorVersion: "1", SampleID: uuid.NewString(), CapturedAt: time.Now().UTC().Add(-time.Minute), CadenceMS: 30000, State: "unsupported"}
	encoded, err := json.Marshal(report.Inventory())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := report.Digest()
	if err != nil {
		t.Fatal(err)
	}
	row := metricRecord{Measurement: model.AcceleratorSampleMeasurement, TimestampNS: report.CapturedAt.UnixNano(), Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"sample_id": mts.StringValue(report.SampleID), "payload_digest": mts.StringValue(digest), "cadence_ms": mts.Int64Value(report.CadenceMS), "inventory": mts.StringValue(string(encoded))}}
	if !validCatalogMetricRecord(row, math.MaxInt64, acceleratorArchiveDefinitions()) {
		t.Fatal("valid GPU metadata rejected")
	}
	row.TimestampNS++
	if validCatalogMetricRecord(row, math.MaxInt64, acceleratorArchiveDefinitions()) {
		t.Fatal("sample time mismatch accepted")
	}
	row.TimestampNS--
	row.Fields["cadence_ms"] = mts.Int64Value(60000)
	if validCatalogMetricRecord(row, math.MaxInt64, acceleratorArchiveDefinitions()) {
		t.Fatal("cadence mismatch accepted")
	}
}

func writeAcceleratorArchiveRows(t *testing.T, rows []metricRecord) string {
	t.Helper()
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(raw))
	}
	path := filepath.Join(t.TempDir(), "gpu.jsonl.gz")
	writeMetricLines(t, path, lines)
	return path
}

func TestAcceleratorArchiveMemoryPairAndRetention(t *testing.T) {
	for _, mode := range []string{"valid", "overflow", "other-device", "other-time", "retained-used-only"} {
		t.Run(mode, func(t *testing.T) {
			used, total := acceleratorArchiveValue("gpu_memory_used_bytes", 4), acceleratorArchiveValue("gpu_memory_total_bytes", 8)
			total.TimestampNS = used.TimestampNS
			if mode != "valid" {
				total.Fields["value"] = mts.Float64Value(2)
			}
			if mode == "other-device" {
				total.Tags["device"] = strings.Repeat("b", 32)
			}
			if mode == "other-time" {
				total.TimestampNS++
			}
			rows := []metricRecord{total, used}
			if mode == "retained-used-only" {
				rows = []metricRecord{used}
			}
			path := writeAcceleratorArchiveRows(t, rows)
			_, err := validateCatalogMetrics(path, math.MaxInt64, acceleratorArchiveDefinitions())
			if (err != nil) != (mode == "overflow") {
				t.Fatal("unexpected pair validation", err)
			}
			matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".gpu-archive-check-*"))
			if err != nil || len(matches) != 0 {
				t.Fatal("temporary GPU MTS was not cleaned", err)
			}
		})
	}
}

func TestAcceleratorArchiveBatchAndCancellation(t *testing.T) {
	rows := make([]metricRecord, acceleratorArchiveBatch+1)
	for index := range rows {
		rows[index] = acceleratorArchiveValue("gpu_memory_used_bytes", float64(index))
		rows[index].TimestampNS += int64(index)
	}
	path := writeAcceleratorArchiveRows(t, rows)
	count, err := validateCatalogMetrics(path, math.MaxInt64, acceleratorArchiveDefinitions())
	if err != nil || count != int64(len(rows)) {
		t.Fatal("batched GPU archive failed", count, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = validateCatalogMetricsContext(ctx, metricValidationInput{path: path, cutoff: math.MaxInt64, definitions: acceleratorArchiveDefinitions()})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("validation ignored cancellation", err)
	}
}
