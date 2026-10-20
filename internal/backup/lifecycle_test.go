package backup

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/model"
)

type lifecycleProbe struct {
	held, acquisitions atomic.Int32
	err                error
}

func (probe *lifecycleProbe) AcquireBackup(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if probe.err != nil {
		return nil, probe.err
	}
	probe.acquisitions.Add(1)
	probe.held.Add(1)
	var once sync.Once
	return func() { once.Do(func() { probe.held.Add(-1) }) }, nil
}

type guardedRecords struct {
	recordStore
	guard *lifecycleProbe
}

func (records guardedRecords) Create(ctx context.Context, record *model.BackupRecord) error {
	if records.guard.held.Load() != 1 {
		return errors.New("record created without lifecycle guard")
	}
	return records.recordStore.Create(ctx, record)
}

func (records guardedRecords) Update(ctx context.Context, record *model.BackupRecord) error {
	if records.guard.held.Load() != 1 {
		return errors.New("record persisted without lifecycle guard")
	}
	return records.recordStore.Update(ctx, record)
}

func TestBackupLifecycleCoversCreationPersistenceAndRestoreStaging(t *testing.T) {
	fixture := newBackupFixture(t)
	probe := new(lifecycleProbe)
	fixture.service.SetSecretLifecycle(probe)
	fixture.service.records = guardedRecords{recordStore: fixture.service.records, guard: probe}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.Wait()
	if probe.held.Load() != 0 || probe.acquisitions.Load() != 1 {
		t.Fatal("backup guard leaked")
	}
	if _, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: record.ID,
		Confirmation: RestoreConfirmation}); err != nil {
		t.Fatal(err)
	}
	if probe.held.Load() != 0 || probe.acquisitions.Load() != 2 {
		t.Fatal("stage guard leaked")
	}
	if _, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: "missing",
		Confirmation: RestoreConfirmation}); err == nil {
		t.Fatal("missing archive staged")
	}
	if probe.held.Load() != 0 {
		t.Fatal("failed stage leaked guard")
	}
}

func TestBusyLifecyclePreventsAnyBackupRecordOrRestoreMutation(t *testing.T) {
	fixture := newBackupFixture(t)
	probe := &lifecycleProbe{err: applicationsecrets.ErrLifecycleBusy}
	fixture.service.SetSecretLifecycle(probe)
	if _, err := fixture.service.Start(t.Context()); !errors.Is(err, applicationsecrets.ErrLifecycleBusy) {
		t.Fatalf("busy backup: %v", err)
	}
	if _, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: "missing",
		Confirmation: RestoreConfirmation}); !errors.Is(err, applicationsecrets.ErrLifecycleBusy) {
		t.Fatalf("busy restore: %v", err)
	}
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 0 {
		t.Fatal("busy lifecycle created a record")
	}
	if probe.held.Load() != 0 {
		t.Fatal("busy lifecycle leaked lock")
	}
}
