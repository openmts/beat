package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestAgentArchivePreservesLegacyCutoffAndRejectsOverflow(t *testing.T) {
	now := time.Now().UTC()
	for _, version := range []int{1, 2} {
		end, err := archiveMetricCutoff(model.BackupManifest{FormatVersion: version, SnapshotCutoff: now})
		if err != nil || !end.Equal(now) {
			t.Fatal("legacy cutoff was widened", err)
		}
	}
	if _, err := archiveMetricCutoff(model.BackupManifest{FormatVersion: 3}); err == nil {
		t.Fatal("invalid archive cutoff accepted")
	}
}

func TestFailedPlatformCaptureReleasesMTSFreeze(t *testing.T) {
	fixture := newBackupFixture(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, sqliteEntry), []byte("existing snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.capturePlatformData(t.Context(), workspace); err == nil {
		t.Fatal("existing snapshot was overwritten")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	guard, err := fixture.mts.BeginMetricSnapshot(ctx)
	if err != nil {
		t.Fatal("failed capture retained MTS freeze", err)
	}
	guard.Close()
}

func TestAgentMetricExportRejectsInvalidCutoffOverwriteAndClosedEngine(t *testing.T) {
	fixture := newBackupFixture(t)
	workspace := t.TempDir()
	if err := fixture.sqlite.Snapshot(t.Context(), filepath.Join(workspace, sqliteEntry)); err != nil {
		t.Fatal(err)
	}
	input := metricArchiveInput{path: filepath.Join(workspace, metricsEntry)}
	if _, err := fixture.service.writeSnapshotMetrics(t.Context(), input, fixture.mts.ExportCatalogRows); err == nil {
		t.Fatal("invalid cutoff was exported")
	}
	input.cutoff = time.Now()
	if err := os.WriteFile(input.path, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.writeSnapshotMetrics(t.Context(), input, fixture.mts.ExportCatalogRows); err == nil {
		t.Fatal("existing metric export was overwritten")
	}
	content, err := os.ReadFile(input.path)
	if err != nil || string(content) != "preserve" {
		t.Fatal("failed export changed preexisting file")
	}
	if err := fixture.mts.Close(); err != nil {
		t.Fatal(err)
	}
	input.path = filepath.Join(workspace, "closed.jsonl.gz")
	if _, err := fixture.service.writeSnapshotMetrics(t.Context(), input, fixture.mts.ExportCatalogRows); !errors.Is(err, store.ErrMTSStoreClosed) {
		t.Fatal("closed engine became successful empty export", err)
	}
}
