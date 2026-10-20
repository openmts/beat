package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestBackupMetricInventoryUsesFrozenSQLiteAndRejectsMismatchedManifest(t *testing.T) {
	fixture := customHistoryBackup(t)
	workspace := t.TempDir()
	path := filepath.Join(workspace, sqliteEntry)
	if err := fixture.sqlite.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	frozen, err := snapshotMetricCatalog(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	customs := store.NewCustomMetricStore(fixture.sqlite.DB)
	if _, err := customs.Create(t.Context(), model.CustomMetric{Name: "late", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.mts.WriteMetric(t.Context(), "node", "custom_late", 9, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	rows, err := fixture.service.writeMetrics(t.Context(), filepath.Join(workspace, metricsEntry), time.Now())
	if err != nil || rows != 1 {
		t.Fatalf("export used changing live inventory: %d/%v", rows, err)
	}
	manifest := model.BackupManifest{FormatVersion: 3, CatalogVersion: frozen.Version, CatalogRevision: frozen.Revision}
	if _, err := archivedMetricInventory(t.Context(), workspace, manifest); err != nil {
		t.Fatal(err)
	}
	manifest.CatalogRevision++
	if _, err := archivedMetricInventory(t.Context(), workspace, manifest); err == nil {
		t.Fatal("mismatched catalog revision accepted")
	}
	if _, err := snapshotMetricCatalog(t.Context(), filepath.Join(workspace, "missing")); err == nil {
		t.Fatal("missing catalog returned success")
	}
	if inventory, err := archivedMetricInventory(t.Context(), workspace, model.BackupManifest{FormatVersion: 1}); err != nil || inventory != nil {
		t.Fatal("legacy archive inventory changed")
	}
}

func TestBackupManifestRejectsTrailingAndOversizedMetadata(t *testing.T) {
	manifest := model.BackupManifest{FormatVersion: 3, CatalogVersion: metriccatalog.Version, CatalogRevision: 1, CreatedAt: time.Now(), SnapshotCutoff: time.Now()}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), manifestEntry)
	for _, body := range []string{string(encoded) + " {}", string(encoded) + strings.Repeat(" ", maximumManifestBytes)} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readManifest(path); err == nil {
			t.Fatal("unbounded or ambiguous manifest accepted")
		}
	}
	manifest.CatalogVersion++
	if err := validateManifestHeader(manifest); err == nil {
		t.Fatal("unknown catalog version accepted")
	}
}

func TestBackupCatalogValidationRejectsUnknownDomainAndInvalidMetadata(t *testing.T) {
	definitions := map[string]model.MetricDefinition{}
	for _, definition := range metriccatalog.Builtins() {
		definitions[definition.Key] = definition
	}
	now := time.Now().UnixNano()
	record := metricRecord{Measurement: "cpu", TimestampNS: now, Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": mts.Float64Value(101)}}
	if validCatalogMetricRecord(record, now, definitions) {
		t.Fatal("out-of-domain archived value accepted")
	}
	record.Fields["value"] = mts.Float64Value(-1)
	if validCatalogMetricRecord(record, now, definitions) {
		t.Fatal("negative builtin archived value accepted")
	}
	record.Measurement = "custom_unknown"
	record.Fields["value"] = mts.Float64Value(1)
	if validCatalogMetricRecord(record, now, definitions) {
		t.Fatal("unknown archived custom measurement admitted")
	}
	fixture := newBackupFixture(t)
	path := filepath.Join(t.TempDir(), sqliteEntry)
	if err := fixture.sqlite.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	database, err := openArchivedSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotMetricCatalog(t.Context(), path); err == nil {
		t.Fatal("removed archive snapshot returned success")
	}
}
