package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type fakeRecordStore struct {
	record        *model.BackupRecord
	page          model.BackupPage
	createErr     error
	updateErr     error
	transitionErr error
	getErr        error
	listErr       error
	deleteErr     error
	auditErr      error
	expireErr     error
	quarantineErr error
}

func (records *fakeRecordStore) Create(context.Context, *model.BackupRecord) error {
	return records.createErr
}

func (records *fakeRecordStore) Update(context.Context, *model.BackupRecord) error {
	return records.updateErr
}

func (records *fakeRecordStore) Get(context.Context, string) (*model.BackupRecord, error) {
	return records.record, records.getErr
}

func (records *fakeRecordStore) ListBackupsPage(
	context.Context,
	store.BackupQuery,
) (model.BackupPage, error) {
	return records.page, records.listErr
}

func (records *fakeRecordStore) Delete(context.Context, string) error { return records.deleteErr }

func (records *fakeRecordStore) Transition(context.Context, store.BackupTransition) error {
	return records.transitionErr
}

func (records *fakeRecordStore) Audit(context.Context, *model.AdminAuditEvent) error {
	return records.auditErr
}

func (records *fakeRecordStore) ExpireTerminal(
	context.Context,
	[]string,
	time.Time,
) (int64, error) {
	return 0, records.expireErr
}

func (records *fakeRecordStore) RecordQuarantine(
	context.Context,
	*model.BackupQuarantineRecord,
) error {
	return records.quarantineErr
}

func (records *fakeRecordStore) CountQuarantine(context.Context) (int, error) { return 0, nil }

func (records *fakeRecordStore) ListQuarantine(
	context.Context,
	int,
) ([]model.BackupQuarantineRecord, error) {
	return nil, records.quarantineErr
}

func TestBackupServiceRecordAndFileErrors(t *testing.T) {
	root := t.TempDir()
	records := &fakeRecordStore{}
	service := &Service{records: records, root: root, now: time.Now}
	records.createErr = errors.New("create failed")
	if _, err := service.Start(t.Context()); err == nil {
		t.Fatal("backup started after record creation failed")
	}
	records.listErr = errors.New("list failed")
	if _, err := listBackups(t, service); err == nil {
		t.Fatal("backup list error was ignored")
	}
	records.getErr = errors.New("get failed")
	if _, _, err := service.Open(t.Context(), "backup"); err == nil {
		t.Fatal("backup record lookup error was ignored")
	}
	records.getErr = nil
	records.record = &model.BackupRecord{ID: "backup", Filename: "backup.zip", State: model.BackupStateRunning}
	if _, _, err := service.Open(t.Context(), "backup"); err == nil {
		t.Fatal("running backup opened")
	}
	records.record.State = model.BackupStateReady
	records.record.Filename = "../backup.zip"
	if _, _, err := service.Open(t.Context(), "backup"); err == nil {
		t.Fatal("unsafe backup filename opened")
	}
	records.record.Filename = "missing.zip"
	if _, _, err := service.Open(t.Context(), "backup"); err == nil {
		t.Fatal("missing backup archive opened")
	}
}

func TestBackupServiceDeletePaths(t *testing.T) {
	root := t.TempDir()
	records := &fakeRecordStore{record: &model.BackupRecord{
		ID: "backup", Filename: "backup.zip", State: model.BackupStateReady,
	}}
	service := &Service{records: records, root: root, now: time.Now}
	// 归档缺失属于幂等成功：删除仍然推进状态并移除记录。
	if err := service.Delete(t.Context(), "backup", SystemActor()); err != nil {
		t.Fatalf("delete backup without archive: %v", err)
	}
	archivePath := filepath.Join(root, records.record.Filename)
	writeTestFile(t, archivePath, "archive")
	if err := service.Delete(t.Context(), "backup", SystemActor()); err != nil {
		t.Fatalf("delete backup: %v", err)
	}
	if _, err := os.Lstat(archivePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("archive survived deletion")
	}
	writeTestFile(t, archivePath, "archive")
	records.transitionErr = errors.New("transition failed")
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("delete ignored state transition failure")
	}
	records.transitionErr = nil
	records.deleteErr = errors.New("delete record failed")
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("backup record deletion error was ignored")
	}
	records.deleteErr = nil
	records.auditErr = errors.New("audit failed")
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("delete ignored audit failure")
	}
	records.auditErr = nil
	records.record.Filename = "../outside.zip"
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("unsafe backup filename deleted")
	}
	records.record.Filename = "squatter.zip"
	if err := os.Symlink(filepath.Join(root, "target.zip"), filepath.Join(root, records.record.Filename)); err != nil {
		t.Fatalf("create symlink archive: %v", err)
	}
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("symlinked backup archive was removed")
	}
	records.record.State = model.BackupStateStaged
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("staged backup deleted")
	}
}

func TestBackupFilesystemHelpers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backups")
	if err := secureDirectory(root); err != nil {
		t.Fatalf("secure backup directory: %v", err)
	}
	parentFile := filepath.Join(t.TempDir(), "parent")
	writeTestFile(t, parentFile, "file")
	if err := secureDirectory(filepath.Join(parentFile, "backups")); err == nil {
		t.Fatal("backup directory created below regular file")
	}
	if err := removeRegularFile(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing backup file removal succeeded")
	}
	destination, err := os.CreateTemp(root, "copy-")
	if err != nil {
		t.Fatalf("create copy target: %v", err)
	}
	if _, err := copyBounded(destination, failingReader{}, 10); err == nil {
		t.Fatal("bounded copy ignored source error")
	}
	if err := destination.Close(); err != nil {
		t.Fatalf("close copy target: %v", err)
	}
}

func TestBackupServiceExecutionAndConstructorFailures(t *testing.T) {
	fixture := newBackupFixture(t)
	parentFile := filepath.Join(t.TempDir(), "parent")
	writeTestFile(t, parentFile, "file")
	if _, err := NewService(t.Context(), &fakeRecordStore{}, fixture.sqlite, fixture.mts,
		filepath.Join(parentFile, "backups"), "", "test"); err == nil {
		t.Fatal("backup service created below regular file")
	}
	records := &fakeRecordStore{updateErr: errors.New("update failed")}
	service := &Service{ctx: t.Context(), records: records, root: parentFile, now: time.Now}
	record, err := service.Start(t.Context())
	if err != nil {
		t.Fatalf("start failing backup: %v", err)
	}
	service.Wait()
	if record.State != model.BackupStateRunning || service.running {
		t.Fatalf("failing backup state = %s, running = %v", record.State, service.running)
	}
	records.getErr = errors.New("get failed")
	if err := service.Delete(t.Context(), "backup", SystemActor()); err == nil {
		t.Fatal("delete ignored record lookup failure")
	}
}

func TestUploadAndStageRecordFailures(t *testing.T) {
	fixture := newBackupFixture(t)
	archive, generated := generatedArchive(t, fixture)
	root := filepath.Join(t.TempDir(), "backups")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create upload root: %v", err)
	}
	records := &fakeRecordStore{createErr: errors.New("create record failed")}
	service := &Service{records: records, root: root, now: time.Now}
	if _, err := service.ValidateUpload(t.Context(), bytes.NewReader(archive)); err == nil {
		t.Fatal("uploaded archive persisted after record creation failed")
	}

	records.createErr = nil
	records.record = &model.BackupRecord{ID: "backup", Filename: "backup.zip", State: model.BackupStateRunning}
	if _, err := service.StageRestore(t.Context(), StageRequest{BackupID: "backup",
		Confirmation: RestoreConfirmation}); err == nil {
		t.Fatal("running backup staged for restore")
	}
	records.record.State = model.BackupStateReady
	records.record.Filename = "../backup.zip"
	if _, err := service.StageRestore(t.Context(), StageRequest{BackupID: "backup",
		Confirmation: RestoreConfirmation}); err == nil {
		t.Fatal("unsafe restore archive staged")
	}
	records.record.Filename = generated.Filename
	if err := os.WriteFile(filepath.Join(root, generated.Filename), archive, 0o600); err != nil {
		t.Fatalf("write staged archive: %v", err)
	}
	records.transitionErr = errors.New("update record failed")
	if _, err := service.StageRestore(t.Context(), StageRequest{BackupID: "backup",
		Confirmation: RestoreConfirmation}); err == nil {
		t.Fatal("restore stage ignored record update error")
	}
}

func TestUploadCreationAndCopyFailures(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "root-file")
	writeTestFile(t, rootFile, "file")
	service := &Service{records: &fakeRecordStore{}, root: rootFile, now: time.Now}
	if _, err := service.ValidateUpload(t.Context(), strings.NewReader("archive")); err == nil {
		t.Fatal("upload created below regular file")
	}
	root := t.TempDir()
	service.root = root
	if _, err := service.ValidateUpload(t.Context(), failingReader{}); err == nil {
		t.Fatal("upload ignored source read error")
	}
}
