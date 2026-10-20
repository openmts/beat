package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func newUploadSession(id string, now time.Time) model.BackupUploadSession {
	return model.BackupUploadSession{ID: id, OwnerUserID: "owner-1", Filename: "archive.zip",
		DeclaredBytes:  2 * model.MinimumBackupUploadChunkBytes,
		DeclaredDigest: strings.Repeat("a", 64), ChunkBytes: model.MinimumBackupUploadChunkBytes,
		ChunkCount: 2, StagingName: "u-" + id, State: model.BackupUploadStateOpen,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
}

func TestBackupUploadStoreLifecycle(t *testing.T) {
	database := setupTestDB(t)
	uploadStore := NewBackupUploadStore(database.DB)
	actor := BackupUploadActor{UserID: "owner-1", Username: "owner", RequestID: "request"}
	now := time.Now().UTC()
	session := newUploadSession("session-1", now)
	if err := uploadStore.Create(t.Context(), actor, &session); err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	stored, err := uploadStore.Get(t.Context(), session.ID)
	if err != nil || stored == nil || stored.Filename != "archive.zip" {
		t.Fatalf("get upload session: %v %+v", err, stored)
	}
	if missing, err := uploadStore.Get(t.Context(), "absent"); err != nil || missing != nil {
		t.Fatalf("absent session = %+v, %v", missing, err)
	}
	list, err := uploadStore.ListByOwner(t.Context(), "owner-1", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list upload sessions = %d, %v", len(list), err)
	}
	chunk := model.BackupUploadChunk{SessionID: session.ID, Index: 0, Offset: 0,
		SizeBytes: model.MinimumBackupUploadChunkBytes, Digest: strings.Repeat("b", 64),
		CreatedAt: now}
	if _, inserted, err := uploadStore.AppendChunk(t.Context(), BackupUploadChunkAppend{
		Actor: actor, Session: session, Chunk: chunk, Now: now}); err != nil || !inserted {
		t.Fatalf("append chunk: %v inserted=%v", err, inserted)
	}
	retry, inserted, err := uploadStore.AppendChunk(t.Context(), BackupUploadChunkAppend{
		Actor: actor, Session: session, Chunk: chunk, Now: now})
	if err != nil || inserted || retry.Index != 0 {
		t.Fatalf("idempotent retry: %v inserted=%v", err, inserted)
	}
	conflict := chunk
	conflict.Digest = strings.Repeat("c", 64)
	if _, _, err := uploadStore.AppendChunk(t.Context(), BackupUploadChunkAppend{
		Actor: actor, Session: session, Chunk: conflict, Now: now}); !errors.Is(err, ErrBackupUploadChunkConflict) {
		t.Fatalf("conflicting chunk error = %v", err)
	}
	quarantined, err := uploadStore.Get(t.Context(), session.ID)
	if err != nil || quarantined == nil || quarantined.State != model.BackupUploadStateQuarantined {
		t.Fatalf("quarantined session = %+v, %v", quarantined, err)
	}
	if err := uploadStore.SetState(t.Context(), actor, session.ID,
		model.BackupUploadStateCancelled, "cancelled", now); err != nil {
		t.Fatalf("cancel quarantined session: %v", err)
	}
	if err := uploadStore.SetState(t.Context(), actor, session.ID,
		model.BackupUploadStateExpired, "expired", now); !errors.Is(err, ErrBackupUploadState) {
		t.Fatalf("terminal transition error = %v", err)
	}
	if _, err := database.DB.Exec(`SELECT COUNT(*) FROM admin_audit_events
		WHERE resource_type = 'backup_upload_session'`); err != nil {
		t.Fatalf("query audit: %v", err)
	}
}

func TestBackupUploadStoreEnforcesLimitsAndCleanup(t *testing.T) {
	database := setupTestDB(t)
	uploadStore := NewBackupUploadStore(database.DB)
	actor := BackupUploadActor{UserID: "owner-1"}
	now := time.Now().UTC()
	for index := 0; index < model.MaximumOpenBackupUploadsPerOwner; index++ {
		session := newUploadSession("session-"+string(rune('a'+index)), now)
		if err := uploadStore.Create(t.Context(), actor, &session); err != nil {
			t.Fatalf("create session %d: %v", index, err)
		}
	}
	extra := newUploadSession("session-extra", now)
	if err := uploadStore.Create(t.Context(), actor, &extra); !errors.Is(err, ErrBackupUploadLimit) {
		t.Fatalf("open session limit error = %v", err)
	}
	for index := 0; index < model.MaximumOpenBackupUploadsPerOwner; index++ {
		id := "session-" + string(rune('a'+index))
		if err := uploadStore.SetState(t.Context(), actor, id,
			model.BackupUploadStateCancelled, "cancelled", now); err != nil {
			t.Fatalf("cancel session: %v", err)
		}
	}
	pending, err := uploadStore.PendingStagingCleanup(t.Context(), 10)
	if err != nil || len(pending) != model.MaximumOpenBackupUploadsPerOwner {
		t.Fatalf("pending cleanup = %d, %v", len(pending), err)
	}
	for _, session := range pending {
		if err := uploadStore.MarkStagingRemoved(t.Context(), session.ID, now); err != nil {
			t.Fatalf("mark staging removed: %v", err)
		}
	}
	if err := uploadStore.MarkStagingRemoved(t.Context(), pending[0].ID, now); err == nil {
		t.Fatal("duplicate staging removal accepted")
	}
	if removed, err := uploadStore.Prune(t.Context(), 2, 10); err != nil || removed != 2 {
		t.Fatalf("prune = %d, %v", removed, err)
	}
}

func TestBackupUploadStoreExpireAndStateGuards(t *testing.T) {
	database := setupTestDB(t)
	uploadStore := NewBackupUploadStore(database.DB)
	actor := BackupUploadActor{UserID: "owner-1"}
	now := time.Now().UTC()
	session := newUploadSession("session-1", now)
	session.ExpiresAt = now.Add(-time.Minute)
	if err := uploadStore.Create(t.Context(), actor, &session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if count, err := uploadStore.Expire(t.Context(), now); err != nil || count != 1 {
		t.Fatalf("expire = %d, %v", count, err)
	}
	if err := uploadStore.MarkValidated(t.Context(), actor, session.ID, "backup-1", now); !errors.Is(err, ErrBackupUploadClosed) {
		t.Fatalf("validated expired session error = %v", err)
	}
	if err := uploadStore.MarkValidated(t.Context(), actor, "absent", "backup-1", now); !errors.Is(err, ErrBackupUploadClosed) {
		t.Fatalf("validated missing session error = %v", err)
	}
}

func TestBackupUploadStoreChunkGuards(t *testing.T) {
	database := setupTestDB(t)
	uploadStore := NewBackupUploadStore(database.DB)
	actor := BackupUploadActor{UserID: "owner-1"}
	now := time.Now().UTC()
	if _, _, err := uploadStore.AppendChunk(t.Context(), BackupUploadChunkAppend{Actor: actor,
		Chunk: model.BackupUploadChunk{SessionID: "absent", Index: 0}, Now: now}); !errors.Is(err, ErrBackupUploadClosed) {
		t.Fatalf("missing session error = %v", err)
	}
	session := newUploadSession("session-1", now)
	session.ExpiresAt = now.Add(-time.Second)
	if err := uploadStore.Create(t.Context(), actor, &session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	chunk := model.BackupUploadChunk{SessionID: session.ID, Index: 0, Offset: 0,
		SizeBytes: model.MinimumBackupUploadChunkBytes, Digest: strings.Repeat("b", 64),
		CreatedAt: now}
	if _, _, err := uploadStore.AppendChunk(t.Context(), BackupUploadChunkAppend{Actor: actor,
		Session: session, Chunk: chunk, Now: now}); !errors.Is(err, ErrBackupUploadClosed) {
		t.Fatalf("expired session error = %v", err)
	}
	expired, err := uploadStore.Get(t.Context(), session.ID)
	if err != nil || expired == nil || expired.State != model.BackupUploadStateExpired {
		t.Fatalf("expired session = %+v, %v", expired, err)
	}
	open := newUploadSession("session-2", now)
	if err := uploadStore.Create(t.Context(), actor, &open); err != nil {
		t.Fatalf("create second session: %v", err)
	}
	outOfRange := chunk
	outOfRange.SessionID = open.ID
	outOfRange.Index = 5
	if _, _, err := uploadStore.AppendChunk(t.Context(), BackupUploadChunkAppend{Actor: actor,
		Session: open, Chunk: outOfRange, Now: now}); !errors.Is(err, ErrBackupUploadState) {
		t.Fatalf("out of range chunk error = %v", err)
	}
	if chunks, err := uploadStore.ListChunks(t.Context(), open.ID); err != nil || len(chunks) != 0 {
		t.Fatalf("list chunks = %d, %v", len(chunks), err)
	}
	if chunk, err := uploadStore.Chunk(t.Context(), open.ID, 0); err != nil || chunk != nil {
		t.Fatalf("absent chunk = %+v, %v", chunk, err)
	}
	if err := uploadStore.MarkValidated(t.Context(), actor, open.ID, "backup-1", now); err != nil {
		t.Fatalf("mark validated: %v", err)
	}
	if chunks, err := uploadStore.ListChunks(t.Context(), "absent"); err != nil || len(chunks) != 0 {
		t.Fatalf("absent list chunks = %d, %v", len(chunks), err)
	}
}
