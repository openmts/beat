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

// pagingRecordStore 按调用顺序返回预置分页，用于驱动对账的游标循环。
type pagingRecordStore struct {
	*fakeRecordStore
	pages []model.BackupPage
	calls int
}

func (records *pagingRecordStore) ListBackupsPage(
	context.Context,
	store.BackupQuery,
) (model.BackupPage, error) {
	page := records.pages[min(records.calls, len(records.pages)-1)]
	records.calls++
	return page, nil
}

func newFaultService(root string, records recordStore) *Service {
	return &Service{records: records, root: root, now: func() time.Time { return time.Now().UTC() }}
}

func TestReconcileFailsClosedOnJournalAndStoreFaults(t *testing.T) {
	root := t.TempDir()
	records := &fakeRecordStore{}
	service := newFaultService(root, records)
	journal := filepath.Join(filepath.Dir(root), "restore.pending.json")
	if err := os.WriteFile(journal, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}
	if _, err := service.Reconcile(t.Context()); err == nil {
		t.Fatal("corrupt restore journal accepted")
	}
	if err := os.Remove(journal); err != nil {
		t.Fatalf("remove corrupt journal: %v", err)
	}

	records.listErr = errors.New("list failed")
	if _, err := service.Reconcile(t.Context()); !errors.Is(err, records.listErr) {
		t.Fatalf("reconcile list error = %v", err)
	}
	records.listErr = nil

	records.page = model.BackupPage{Items: []model.BackupRecord{
		{ID: "backup-1", Filename: "backup-1.zip", State: model.BackupStateRunning}}}
	records.transitionErr = errors.New("transition failed")
	if _, err := service.Reconcile(t.Context()); !errors.Is(err, records.transitionErr) {
		t.Fatalf("reconcile transition error = %v", err)
	}
	records.transitionErr = store.ErrBackupStateConflict
	outcome, err := service.Reconcile(t.Context())
	if err != nil || outcome.Deferred != 1 {
		t.Fatalf("deferred outcome = %#v, %v", outcome, err)
	}
	records.transitionErr = nil

	records.expireErr = errors.New("expire failed")
	if _, err := service.Reconcile(t.Context()); !errors.Is(err, records.expireErr) {
		t.Fatalf("reconcile expire error = %v", err)
	}
}

func TestReconcileFailsClosedWhenBackupRootIsNotADirectory(t *testing.T) {
	parent := t.TempDir()
	rootFile := filepath.Join(parent, "backups")
	if err := os.WriteFile(rootFile, []byte("file"), 0o600); err != nil {
		t.Fatalf("write backup root: %v", err)
	}
	service := newFaultService(rootFile, &fakeRecordStore{})
	if _, err := service.Reconcile(t.Context()); err == nil {
		t.Fatal("non-directory backup root accepted")
	}
}

func TestReconcileListAllRecordsFollowsCursor(t *testing.T) {
	root := t.TempDir()
	records := &pagingRecordStore{fakeRecordStore: &fakeRecordStore{}, pages: []model.BackupPage{
		{NextCursor: "page-2"},
		{Items: []model.BackupRecord{{ID: "backup-1", Filename: "backup-1.zip"}}},
	}}
	service := newFaultService(root, records)
	items, err := service.listAllRecords(t.Context())
	if err != nil || len(items) != 1 || records.calls != 2 {
		t.Fatalf("listAllRecords = %#v, %v, calls = %d", items, err, records.calls)
	}
}

func TestReconcileDefersQuarantineInventoryFaults(t *testing.T) {
	root := t.TempDir()
	orphan := filepath.Join(root, "beat-orphan.zip")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan archive: %v", err)
	}
	records := &fakeRecordStore{quarantineErr: errors.New("inventory failed")}
	service := newFaultService(root, records)
	outcome, err := service.Reconcile(t.Context())
	if err != nil || outcome.Deferred != 1 || outcome.Quarantined != 0 {
		t.Fatalf("quarantine fault outcome = %#v, %v", outcome, err)
	}
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan archive was not moved: %v", err)
	}
}

func TestReconcilerArchiveHelpersRejectUnsafeFilenames(t *testing.T) {
	service := newFaultService(t.TempDir(), &fakeRecordStore{})
	reconciler := &reconciler{service: service, ctx: t.Context()}
	if present, err := reconciler.archiveIsRegular("../escape.zip"); err == nil || present {
		t.Fatalf("unsafe regular check = %v, %v", present, err)
	}
	if reconciler.archiveIsComplete("../escape.zip") {
		t.Fatal("unsafe filename treated as complete archive")
	}
	if reconciler.archiveIsComplete("missing.zip") {
		t.Fatal("missing archive treated as complete")
	}
}
