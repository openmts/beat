package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestListQuarantinedUploadsUsesInjectedStore(t *testing.T) {
	service := &Service{uploads: faultyUploadStore{fail: map[string]error{}}}
	sessions, err := service.ListQuarantinedUploads(t.Context())
	if err != nil || len(sessions) != 0 {
		t.Fatalf("quarantined uploads = %#v, %v", sessions, err)
	}
	withoutUploads := &Service{}
	if sessions, err := withoutUploads.ListQuarantinedUploads(t.Context()); err != nil || len(sessions) != 0 {
		t.Fatalf("missing upload store = %#v, %v", sessions, err)
	}
}

func TestFinalizeUploadFailsClosedOnChunkListingFaults(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	fixture.service.uploads = faultyUploadStore{inner: fixture.service.uploads,
		fail: map[string]error{"ListChunks": errUploadFault}}
	if _, err := fixture.service.FinalizeUpload(t.Context(), actor, session.ID); !errors.Is(err, errUploadFault) {
		t.Fatalf("chunk listing fault error = %v", err)
	}
}

func TestPublishUploadRejectsMissingTemporaryAndInvalidArchive(t *testing.T) {
	root := t.TempDir()
	service := newFaultService(root, &fakeRecordStore{})
	service.uploads = faultyUploadStore{fail: map[string]error{"SetState": errUploadFault}}
	actor := uploadActor()
	session := &model.BackupUploadSession{ID: "session-1", OwnerUserID: actor.UserID,
		StagingName: "staging-1"}

	if _, err := service.publishUpload(t.Context(), actor, session,
		filepath.Join(root, "absent.tmp"), 5); err == nil {
		t.Fatal("missing temporary archive was published")
	}

	temporary := filepath.Join(root, "garbage.tmp")
	if err := os.WriteFile(temporary, []byte("not a zip"), 0o600); err != nil {
		t.Fatalf("write garbage archive: %v", err)
	}
	if _, err := service.publishUpload(t.Context(), actor, session, temporary, 8); err == nil {
		t.Fatal("invalid uploaded archive was accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".zip" {
			t.Fatalf("rejected archive survived as %s", entry.Name())
		}
	}
}

func TestPublishUploadFailsClosedWhenRecordCreationFails(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	source := filepath.Join(fixture.service.root, record.Filename)
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read fixture archive: %v", err)
	}
	temporary := filepath.Join(fixture.service.root, ".upload-retry")
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		t.Fatalf("write temporary archive: %v", err)
	}
	fixture.service.records = &fakeRecordStore{createErr: errUploadFault}
	actor := uploadActor()
	session := &model.BackupUploadSession{ID: "session-1", OwnerUserID: actor.UserID,
		StagingName: "staging-1"}
	if _, err := fixture.service.publishUpload(t.Context(), actor, session, temporary, int64(len(content))); !errors.Is(err, errUploadFault) {
		t.Fatalf("record creation fault error = %v", err)
	}
	entries, err := os.ReadDir(fixture.service.root)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != record.Filename {
			t.Fatalf("unpublished upload survived as %s", entry.Name())
		}
	}
}

func TestStageRestoreRejectsCorruptArchiveFile(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	if err := os.WriteFile(filepath.Join(fixture.service.root, record.Filename),
		[]byte("not a zip"), 0o600); err != nil {
		t.Fatalf("corrupt backup archive: %v", err)
	}
	if _, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: record.ID,
		Confirmation: RestoreConfirmation, Actor: SystemActor()}); err == nil {
		t.Fatal("corrupt backup archive was staged")
	}
}
