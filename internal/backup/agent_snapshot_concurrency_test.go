package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestBackupFreezesCutoffAndExcludesConcurrentFutureAgentWrite(t *testing.T) {
	fixture := newBackupFixture(t)
	now := time.Now().UTC()
	first := backupAgentInput(now)
	first.Report.CapturedAt = now.Add(time.Minute)
	if err := writeBackupAgentSample(t.Context(), fixture.mts, first); err != nil {
		t.Fatal(err)
	}
	frozen, proceed := make(chan struct{}), make(chan struct{})
	fixture.service.now = func() time.Time { close(frozen); <-proceed; return now }
	workspace := t.TempDir()
	captured := make(chan error, 1)
	go func() { _, err := fixture.service.capturePlatformData(t.Context(), workspace); captured <- err }()
	<-frozen
	later := backupAgentInput(now)
	later.Report.CapturedAt = first.Report.CapturedAt.Add(time.Second)
	written := make(chan error, 1)
	go func() { written <- writeBackupAgentSample(t.Context(), fixture.mts, later) }()
	verifyFrozenMetricOperationsCancel(t, fixture)
	select {
	case err := <-written:
		t.Fatal("new write passed snapshot freeze", err)
	default:
	}
	close(proceed)
	if err := <-captured; err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	end, err := model.AgentArchiveEnd(now)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := validateMetrics(filepath.Join(workspace, metricsEntry), end.UnixNano())
	if err != nil || rows != int64(len(model.MetricNames())+3) {
		t.Fatal("post-cutoff future report entered frozen archive", rows, err)
	}
	receipt, err := fixture.mts.LatestAgentReceipt(t.Context(), later.Identity.NodeID)
	if err != nil || receipt == nil || receipt.SampleID != later.Report.SampleID {
		t.Fatal("blocked ingestion did not resume after snapshot", err)
	}
}

func verifyFrozenMetricOperationsCancel(t *testing.T, fixture *backupFixture) {
	t.Helper()
	operations := []func(context.Context) error{
		func(ctx context.Context) error { _, err := fixture.mts.QueryLatest(ctx, "backup-node"); return err },
		func(ctx context.Context) error { return fixture.mts.CleanupBefore(ctx, time.Now().Add(-time.Hour)) },
		func(ctx context.Context) error {
			snapshot, err := fixture.mts.BeginMetricSnapshot(ctx)
			if err == nil {
				snapshot.Close()
			}
			return err
		},
	}
	for _, operation := range operations {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		err := operation(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("snapshot-blocked operation ignored cancellation", err)
		}
	}
}
