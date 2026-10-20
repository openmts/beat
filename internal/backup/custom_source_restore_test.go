package backup

import (
	"context"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestBackupV3RestoresActualCustomSourceAlongsideNumericValue(t *testing.T) {
	fixture := newBackupFixture(t)
	metric, err := store.NewCustomMetricStore(fixture.sqlite.DB).Create(t.Context(), model.CustomMetric{Name: "backup_queue", ShellCommand: "printf 7", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input := backupAgentInput(now)
	input.Report.CustomMetrics = map[string]float64{metric.Name: 7}
	source := testutil.CustomSourceFixture(now.Add(-20*time.Second), *metric)
	input.Report.CustomSamples = map[string]model.CustomSampleMetadata{metric.Name: source}
	attempt, err := fixture.mts.PrepareAgentReport(t.Context(), input, func(context.Context) (store.MetricReportSample, error) {
		return store.MetricReportSample{Custom: []model.MetricValue{{Name: "custom_backup_queue", Value: 7}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, commitErr := attempt.Commit(t.Context())
	attempt.Close()
	if commitErr != nil {
		t.Fatal(commitErr)
	}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	rows, err := listBackups(t, fixture.service)
	if err != nil || rows[0].State != model.BackupStateReady {
		t.Fatal("custom source backup failed", rows, err)
	}
	restored := restoreAgentBackup(t, fixture, record)
	evidence, err := restored.QueryCustomSample(t.Context(), input.Identity.NodeID, "custom_backup_queue")
	if err != nil || evidence == nil || evidence.Value != 7 || evidence.Source != source || !evidence.ReportAt.Equal(now) {
		t.Fatal("restore lost actual custom identity/cadence/version", err)
	}
}
