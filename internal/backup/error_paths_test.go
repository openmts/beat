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
)

type fakeRecordStore struct {
	record    *model.BackupRecord
	createErr error
	updateErr error
	getErr    error
	listErr   error
	deleteErr error
}

func (store *fakeRecordStore) Create(context.Context, *model.BackupRecord) error {
	return store.createErr
}

func (store *fakeRecordStore) Update(context.Context, *model.BackupRecord) error {
	return store.updateErr
}

func (store *fakeRecordStore) Get(context.Context, string) (*model.BackupRecord, error) {
	return store.record, store.getErr
}

func (store *fakeRecordStore) List(context.Context) ([]model.BackupRecord, error) {
	return nil, store.listErr
}

func (store *fakeRecordStore) Delete(context.Context, string) error { return store.deleteErr }

func TestBackupServiceRecordAndFileErrors(t *testing.T) {
	root := t.TempDir()
	records := &fakeRecordStore{}
	service := &Service{records: records, root: root, now: time.Now}
	records.createErr = errors.New("create failed")
	if _, err := service.Start(t.Context()); err == nil {
		t.Fatal("backup started after record creation failed")
	}
	records.listErr = errors.New("list failed")
	if _, err := service.List(t.Context()); err == nil {
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
	if err := service.Delete(t.Context(), "backup"); err == nil {
		t.Fatal("missing backup archive deleted")
	}
	archivePath := filepath.Join(root, records.record.Filename)
	writeTestFile(t, archivePath, "archive")
	if err := service.Delete(t.Context(), "backup"); err != nil {
		t.Fatalf("delete backup: %v", err)
	}
	writeTestFile(t, archivePath, "archive")
	records.deleteErr = errors.New("delete record failed")
	if err := service.Delete(t.Context(), "backup"); err == nil {
		t.Fatal("backup record deletion error was ignored")
	}
	records.record.Filename = "../outside.zip"
	if err := service.Delete(t.Context(), "backup"); err == nil {
		t.Fatal("unsafe backup filename deleted")
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
	if err := service.Delete(t.Context(), "backup"); err == nil {
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
	if _, err := service.StageRestore(t.Context(), "backup", RestoreConfirmation); err == nil {
		t.Fatal("running backup staged for restore")
	}
	records.record.State = model.BackupStateReady
	records.record.Filename = "../backup.zip"
	if _, err := service.StageRestore(t.Context(), "backup", RestoreConfirmation); err == nil {
		t.Fatal("unsafe restore archive staged")
	}
	records.record.Filename = generated.Filename
	if err := os.WriteFile(filepath.Join(root, generated.Filename), archive, 0o600); err != nil {
		t.Fatalf("write staged archive: %v", err)
	}
	records.updateErr = errors.New("update record failed")
	if _, err := service.StageRestore(t.Context(), "backup", RestoreConfirmation); err == nil {
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
