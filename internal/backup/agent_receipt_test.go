package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func backupAgentInput(at time.Time) store.AgentReportInput {
	return store.AgentReportInput{Now: at, Identity: model.AgentIdentity{NodeID: "backup-node", Mode: model.AgentCredentialLegacy, CredentialFingerprint: strings.Repeat("a", 64)},
		Report: model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: at, ReportIntervalMS: 5000, Host: "192.0.2.1", Port: 22, Metrics: &model.NodeMetrics{CPU: 17, NetRecvTotal: 100, NetSentTotal: 50}}}
}

func writeBackupAgentSample(ctx context.Context, metrics *store.MTSStore, input store.AgentReportInput) error {
	attempt, err := metrics.PrepareAgentReport(ctx, input, nil)
	if err != nil {
		return err
	}
	defer attempt.Close()
	_, err = attempt.Commit(ctx)
	return err
}

func restoreAgentBackup(t *testing.T, fixture *backupFixture, record model.BackupRecord) *store.MTSStore {
	t.Helper()
	destination := t.TempDir()
	archiveRoot := filepath.Join(destination, "backups")
	if err := os.Mkdir(archiveRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(archiveRoot, record.Filename)
	if err := copyFile(filepath.Join(fixture.service.root, record.Filename), archivePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJournal(filepath.Join(destination, "restore.pending.json"), restoreJournal(record.ID, archivePath)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPending(t.Context(), destination, filepath.Join(destination, "beat.db"), filepath.Join(destination, "mts")); err != nil {
		t.Fatal(err)
	}
	metrics, err := store.NewMTSStore(filepath.Join(destination, "mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metrics.Close() })
	return metrics
}

func TestBackupV3RestoresFutureAgentReceiptAtClockSkewBoundary(t *testing.T) {
	fixture := newBackupFixture(t)
	now := time.Now().UTC()
	fixture.service.now = func() time.Time { return now }
	input := backupAgentInput(now)
	input.Report.CapturedAt = now.Add(model.MaximumAgentClockSkew)
	if err := writeBackupAgentSample(t.Context(), fixture.mts, input); err != nil {
		t.Fatal(err)
	}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 1 || records[0].State != model.BackupStateReady {
		t.Fatalf("future archive state: %+v %v", records, err)
	}
	if records[0].MetricRows != int64(len(model.MetricNames())+3) {
		t.Fatal("future numeric samples or typed receipt omitted", records[0].MetricRows)
	}
	manifest := backupCatalogManifest(t, filepath.Join(fixture.service.root, record.Filename))
	if manifest.FormatVersion != 4 || !manifest.SnapshotCutoff.Equal(now) {
		t.Fatal("archive envelope or server cutoff changed")
	}
	restored := restoreAgentBackup(t, fixture, record)
	receipt, err := restored.LatestAgentReceipt(t.Context(), input.Identity.NodeID)
	if err != nil || receipt == nil || receipt.SampleID != input.Report.SampleID || !receipt.CapturedAt.Equal(input.Report.CapturedAt) {
		t.Fatal("future receipt did not restore", err)
	}
	values, err := restored.QueryMetrics(t.Context(), []string{"cpu", "net_recv_total"}, now, now.Add(3*time.Minute), input.Identity.NodeID)
	if err != nil || len(values["cpu"]) != 1 || values["cpu"][0].Value != 17 || values["net_recv_total"][0].Value != 100 {
		t.Fatal("restored receipt lost corresponding numeric values", err)
	}
	attempt, err := restored.PrepareAgentReport(t.Context(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	duplicate, err := attempt.Commit(t.Context())
	if err != nil || !duplicate {
		t.Fatal("restored sample was not idempotent", err)
	}
}
