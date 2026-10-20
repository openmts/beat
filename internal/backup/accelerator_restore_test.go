package backup

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func acceleratorRestoreReport() model.AcceleratorReport {
	keys := model.AcceleratorMetricKeys
	values := []float64{75, 8 << 30, 16 << 30, 65, 100, 200}
	metrics := map[string]float64{}
	for index, key := range keys {
		metrics[key] = values[index]
	}
	return model.AcceleratorReport{InstallationEnabled: true, Version: 1, SampleID: uuid.NewString(), CapturedAt: time.Now().UTC().Add(-time.Minute), CadenceMS: 30000, CollectorVersion: "1", State: "ok",
		Capabilities: []model.AcceleratorCapability{{Vendor: "nvidia", State: "ok", Metrics: keys}},
		Devices:      []model.AcceleratorDeviceReport{{Fingerprint: strings.Repeat("a", 64), Vendor: "nvidia", Model: "Fixture GPU", Capabilities: keys, Metrics: metrics}}}
}

func acceleratorRestorePoints(t *testing.T, report model.AcceleratorReport) []mts.Point {
	t.Helper()
	if err := report.Validate(report.CapturedAt); err != nil {
		t.Fatal(err)
	}
	device := report.Devices[0]
	points := make([]mts.Point, 0, len(device.Metrics)+1)
	for key, value := range device.Metrics {
		points = append(points, mts.Point{Measurement: key, Tags: map[string]string{"node": "gpu-node", "device": model.AcceleratorDeviceID("gpu-node", device.Fingerprint), "vendor": device.Vendor}, Timestamp: report.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(value)}})
	}
	inventory, err := json.Marshal(report.Inventory())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := report.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return append(points, mts.Point{Measurement: model.AcceleratorSampleMeasurement, Tags: map[string]string{"node": "gpu-node"}, Timestamp: report.CapturedAt.UnixNano(), Precision: mts.PrecisionNanosecond,
		Fields: map[string]mts.FieldValue{"sample_id": mts.StringValue(report.SampleID), "payload_digest": mts.StringValue(digest), "cadence_ms": mts.Int64Value(report.CadenceMS), "inventory": mts.StringValue(string(inventory))}})
}

func TestAcceleratorBackupV4PreservesEveryMetricAndReceipt(t *testing.T) {
	fixture := newBackupFixture(t)
	report := acceleratorRestoreReport()
	if err := fixture.mts.ImportPoints(t.Context(), acceleratorRestorePoints(t, report)); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != model.BackupStateReady || records[0].MetricRows != 7 {
		t.Fatal("GPU archive omitted series or receipt", records, err)
	}
	manifest := backupCatalogManifest(t, filepath.Join(fixture.service.root, record.Filename))
	if manifest.FormatVersion != 4 {
		t.Fatal("GPU changed backup envelope")
	}
	restored := restoreAgentBackup(t, fixture, record)
	receipt, err := restored.LatestAcceleratorSample(t.Context(), "gpu-node")
	if err != nil || receipt == nil || receipt.Snapshot.SampleID != report.SampleID || !receipt.Snapshot.CapturedAt.Equal(report.CapturedAt) {
		t.Fatal("GPU receipt did not survive restore", err)
	}
	catalog, err := store.NewMetricCatalogStore(fixture.sqlite.DB).Catalog(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	_, err = restored.ExportCatalogRows(t.Context(), store.MetricExport{Cutoff: time.Now().UTC(), Definitions: catalog.Metrics}, func(row mts.Row) error {
		if model.ValidAcceleratorMetric(row.Measurement) {
			if row.Timestamp != report.CapturedAt.UnixNano() || row.Tags["device"] != model.AcceleratorDeviceID("gpu-node", report.Devices[0].Fingerprint) || row.Fields["value"].Float64 != report.Devices[0].Metrics[row.Measurement] {
				t.Error("GPU values/tags/time changed during restore")
			}
			seen[row.Measurement] = true
		}
		return nil
	})
	if err != nil || len(seen) != len(model.AcceleratorMetricKeys) {
		t.Fatal("restored GPU history is incomplete", err)
	}
}
