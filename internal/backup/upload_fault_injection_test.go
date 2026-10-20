package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func bytesReader(content []byte) *bytes.Reader { return bytes.NewReader(content) }

var errUploadFault = errors.New("injected upload failure")

// faultyUploadStore 按方法名注入失败，用来覆盖上传路径的错误分支。
type faultyUploadStore struct {
	inner UploadSessionStore
	fail  map[string]error
}

func (f faultyUploadStore) fault(name string) error { return f.fail[name] }

func (f faultyUploadStore) Create(ctx context.Context, actor store.BackupUploadActor, session *model.BackupUploadSession) error {
	if err := f.fault("Create"); err != nil {
		return err
	}
	return f.inner.Create(ctx, actor, session)
}

func (f faultyUploadStore) Get(ctx context.Context, id string) (*model.BackupUploadSession, error) {
	if err := f.fault("Get"); err != nil {
		return nil, err
	}
	return f.inner.Get(ctx, id)
}

func (f faultyUploadStore) ListByOwner(ctx context.Context, owner string, limit int) ([]model.BackupUploadSession, error) {
	if err := f.fault("ListByOwner"); err != nil {
		return nil, err
	}
	return f.inner.ListByOwner(ctx, owner, limit)
}

func (f faultyUploadStore) ListQuarantined(_ context.Context, _ int) ([]model.BackupUploadSession, error) {
	if err := f.fault("ListQuarantined"); err != nil {
		return nil, err
	}
	return nil, nil
}

func (f faultyUploadStore) SetState(ctx context.Context, actor store.BackupUploadActor, id, state, reason string, now time.Time) error {
	if err := f.fault("SetState"); err != nil {
		return err
	}
	return f.inner.SetState(ctx, actor, id, state, reason, now)
}

func (f faultyUploadStore) MarkValidated(ctx context.Context, actor store.BackupUploadActor, id, recordID string, now time.Time) error {
	if err := f.fault("MarkValidated"); err != nil {
		return err
	}
	return f.inner.MarkValidated(ctx, actor, id, recordID, now)
}

func (f faultyUploadStore) ListChunks(ctx context.Context, id string) ([]model.BackupUploadChunk, error) {
	if err := f.fault("ListChunks"); err != nil {
		return nil, err
	}
	return f.inner.ListChunks(ctx, id)
}

func (f faultyUploadStore) Chunk(ctx context.Context, id string, index int) (*model.BackupUploadChunk, error) {
	if err := f.fault("Chunk"); err != nil {
		return nil, err
	}
	return f.inner.Chunk(ctx, id, index)
}

func (f faultyUploadStore) AppendChunk(ctx context.Context, append store.BackupUploadChunkAppend) (model.BackupUploadChunk, bool, error) {
	if err := f.fault("AppendChunk"); err != nil {
		return model.BackupUploadChunk{}, false, err
	}
	return f.inner.AppendChunk(ctx, append)
}

func (f faultyUploadStore) Expire(ctx context.Context, now time.Time) (int, error) {
	if err := f.fault("Expire"); err != nil {
		return 0, err
	}
	return f.inner.Expire(ctx, now)
}

func (f faultyUploadStore) PendingStagingCleanup(ctx context.Context, limit int) ([]model.BackupUploadSession, error) {
	if err := f.fault("PendingStagingCleanup"); err != nil {
		return nil, err
	}
	return f.inner.PendingStagingCleanup(ctx, limit)
}

func (f faultyUploadStore) MarkStagingRemoved(ctx context.Context, id string, now time.Time) error {
	if err := f.fault("MarkStagingRemoved"); err != nil {
		return err
	}
	return f.inner.MarkStagingRemoved(ctx, id, now)
}

func (f faultyUploadStore) Prune(ctx context.Context, sessions, chunks int) (int, error) {
	if err := f.fault("Prune"); err != nil {
		return 0, err
	}
	return f.inner.Prune(ctx, sessions, chunks)
}

func withUploadFault[T any](fixture *backupFixture, method string, err error, call func() (T, error)) (T, error) {
	real := fixture.service.uploads
	fixture.service.uploads = faultyUploadStore{inner: real, fail: map[string]error{method: err}}
	defer func() { fixture.service.uploads = real }()
	return call()
}

func TestUploadStoreFailureBranches(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	declaration := UploadSessionRequest{Filename: "restore.zip", SizeBytes: int64(len(content)),
		Digest: session.DeclaredDigest}

	if _, err := withUploadFault(fixture, "Create", store.ErrBackupUploadLimit,
		func() (UploadSessionStatus, error) {
			return fixture.service.StartUpload(t.Context(), actor, declaration)
		}); !errors.Is(err, ErrUploadLimit) {
		t.Fatalf("limit error = %v", err)
	}
	if _, err := withUploadFault(fixture, "Create", errUploadFault,
		func() (UploadSessionStatus, error) {
			return fixture.service.StartUpload(t.Context(), actor, declaration)
		}); !errors.Is(err, errUploadFault) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := withUploadFault(fixture, "Get", errUploadFault,
		func() (UploadSessionStatus, error) {
			return fixture.service.UploadStatus(t.Context(), actor, session.ID)
		}); !errors.Is(err, errUploadFault) {
		t.Fatalf("get error = %v", err)
	}
	if _, err := withUploadFault(fixture, "ListChunks", errUploadFault,
		func() (UploadSessionStatus, error) {
			return fixture.service.UploadStatus(t.Context(), actor, session.ID)
		}); !errors.Is(err, errUploadFault) {
		t.Fatalf("list chunks error = %v", err)
	}
	if _, err := withUploadFault(fixture, "SetState", store.ErrBackupUploadState,
		func() (struct{}, error) {
			return struct{}{}, fixture.service.CancelUpload(t.Context(), actor, session.ID)
		}); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("state mapping error = %v", err)
	}
	if _, err := withUploadFault(fixture, "SetState", errUploadFault,
		func() (struct{}, error) {
			return struct{}{}, fixture.service.CancelUpload(t.Context(), actor, session.ID)
		}); !errors.Is(err, errUploadFault) {
		t.Fatalf("set state error = %v", err)
	}
	chunk := content[:model.BackupUploadChunkSize(0, session.ChunkBytes, session.DeclaredBytes)]
	request := UploadChunkRequest{Index: 0, Offset: 0, SizeBytes: int64(len(chunk)),
		Digest: sha256Hex(chunk), Body: nil}
	if _, err := withUploadFault(fixture, "Chunk", errUploadFault, func() (model.BackupUploadChunk, error) {
		request.Body = bytesReader(chunk)
		return fixture.service.UploadChunk(t.Context(), actor, session.ID, request)
	}); !errors.Is(err, errUploadFault) {
		t.Fatalf("chunk lookup error = %v", err)
	}
	for _, test := range []struct {
		name string
		in   error
		want error
	}{
		{"conflict", store.ErrBackupUploadChunkConflict, ErrUploadChunkConflict},
		{"closed", store.ErrBackupUploadState, ErrUploadClosed},
		{"generic", errUploadFault, errUploadFault},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := withUploadFault(fixture, "AppendChunk", test.in, func() (model.BackupUploadChunk, error) {
				request.Body = bytesReader(chunk)
				return fixture.service.UploadChunk(t.Context(), actor, session.ID, request)
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("append error = %v, want %v", err, test.want)
			}
		})
	}
	for _, method := range []string{"Expire", "PendingStagingCleanup", "Prune"} {
		if _, err := withUploadFault(fixture, method, errUploadFault, func() (struct{}, error) {
			return struct{}{}, fixture.service.MaintainUploadSessions(t.Context())
		}); !errors.Is(err, errUploadFault) {
			t.Fatalf("%s maintenance error = %v", method, err)
		}
	}

	uploadAllChunks(t, fixture, actor, session, content)
	if _, err := withUploadFault(fixture, "MarkValidated", errUploadFault, func() (model.BackupRecord, error) {
		return fixture.service.FinalizeUpload(t.Context(), actor, session.ID)
	}); !errors.Is(err, errUploadFault) {
		t.Fatalf("mark validated error = %v", err)
	}
}

func TestUploadChunkExpiredSessionTriggersExpire(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	chunk := content[:model.BackupUploadChunkSize(0, session.ChunkBytes, session.DeclaredBytes)]
	request := UploadChunkRequest{Index: 0, Offset: 0, SizeBytes: int64(len(chunk)),
		Digest: sha256Hex(chunk), Body: bytesReader(chunk)}
	if _, err := fixture.sqlite.DB.Exec(`UPDATE backup_upload_sessions SET expires_at=? WHERE id=?`,
		time.Now().UTC().Add(-time.Hour), session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, request); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("expired chunk error = %v", err)
	}

	real := fixture.service.uploads
	second, err := fixture.service.StartUpload(t.Context(), actor, UploadSessionRequest{Filename: "restore.zip",
		SizeBytes: int64(len(content)), Digest: session.DeclaredDigest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.sqlite.DB.Exec(`UPDATE backup_upload_sessions SET expires_at=? WHERE id=?`,
		time.Now().UTC().Add(-2*time.Hour), second.Session.ID); err != nil {
		t.Fatal(err)
	}
	fixture.service.uploads = faultyUploadStore{inner: real, fail: map[string]error{"Expire": errUploadFault}}
	defer func() { fixture.service.uploads = real }()
	expired := request
	if _, err := fixture.service.UploadChunk(t.Context(), actor, second.Session.ID, expired); !errors.Is(err, errUploadFault) {
		t.Fatalf("expire failure error = %v", err)
	}
}

func TestStartUploadStagingFailureRemovesNothing(t *testing.T) {
	fixture := newUploadFixture(t)
	if err := os.WriteFile(fixture.service.uploadRoot(), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.StartUpload(t.Context(), uploadActor(), UploadSessionRequest{
		Filename: "restore.zip", SizeBytes: 1024, Digest: sha256Hex(make([]byte, 1024))})
	if err == nil {
		t.Fatal("blocked upload root must fail")
	}
	if _, statErr := os.Stat(filepath.Join(fixture.service.root, "uploads")); statErr != nil {
		t.Fatalf("blocked root vanished: %v", statErr)
	}
}
