package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const uploadTestOwner = "owner-1"

func newUploadFixture(t *testing.T) *backupFixture {
	t.Helper()
	fixture := newBackupFixture(t)
	fixture.service.SetUploadSessions(store.NewBackupUploadStore(fixture.sqlite.DB))
	return fixture
}

// largeArchive 生成一个大于单个最小分块的合法归档，用来覆盖多分块装配。
func largeArchive(t *testing.T, fixture *backupFixture) []byte {
	t.Helper()
	points := make([]mts.Point, 0, 120000)
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	random := rand.New(rand.NewSource(7))
	for index := 0; index < 120000; index++ {
		points = append(points, mts.Point{Database: "beat", Measurement: "cpu",
			Tags:      map[string]string{"node": "node-1"},
			Fields:    map[string]mts.FieldValue{"value": mts.Float64Value(random.Float64() * 100)},
			Timestamp: base.Add(time.Duration(index) * time.Millisecond).UnixNano()})
	}
	if err := fixture.mts.ImportPoints(t.Context(), points); err != nil {
		t.Fatalf("import metrics: %v", err)
	}
	record, err := fixture.service.Start(t.Context())
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	fixture.service.Wait()
	stored, err := fixture.service.record(t.Context(), record.ID)
	if err != nil || stored == nil {
		t.Fatalf("read backup record: %v", err)
	}
	if stored.State != model.BackupStateReady {
		t.Fatalf("backup state = %s (%s)", stored.State, stored.ErrorMessage)
	}
	content, err := os.ReadFile(filepath.Join(fixture.service.root, stored.Filename))
	if err != nil {
		t.Fatalf("read backup archive: %v", err)
	}
	if int64(len(content)) <= model.MinimumBackupUploadChunkBytes {
		t.Fatalf("archive is too small for chunk coverage: %d", len(content))
	}
	return content
}

func uploadActor() store.BackupUploadActor {
	return store.BackupUploadActor{UserID: uploadTestOwner, Username: "owner",
		RequestID: "request-1", IPAddress: "127.0.0.1", SessionPrefix: "prefix"}
}

func startUpload(
	t *testing.T,
	fixture *backupFixture,
	content []byte,
) (model.BackupUploadSession, store.BackupUploadActor) {
	t.Helper()
	actor := uploadActor()
	digest := sha256.Sum256(content)
	status, err := fixture.service.StartUpload(t.Context(), actor, UploadSessionRequest{
		Filename: "restore.zip", SizeBytes: int64(len(content)),
		Digest: hex.EncodeToString(digest[:])})
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	return status.Session, actor
}

func uploadChunkAtIndex(
	t *testing.T,
	fixture *backupFixture,
	actor store.BackupUploadActor,
	session model.BackupUploadSession,
	content []byte,
	index int,
) error {
	t.Helper()
	start := model.OffsetForBackupUploadChunk(index, session.ChunkBytes)
	size := model.BackupUploadChunkSize(index, session.ChunkBytes, session.DeclaredBytes)
	chunk := content[start : start+size]
	digest := sha256.Sum256(chunk)
	_, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, UploadChunkRequest{
		Index: index, Offset: start, SizeBytes: size, Digest: hex.EncodeToString(digest[:]),
		Body: bytes.NewReader(chunk)})
	return err
}

func uploadAllChunks(
	t *testing.T,
	fixture *backupFixture,
	actor store.BackupUploadActor,
	session model.BackupUploadSession,
	content []byte,
) {
	t.Helper()
	for index := 0; index < session.ChunkCount; index++ {
		if err := uploadChunkAtIndex(t, fixture, actor, session, content, index); err != nil {
			t.Fatalf("upload chunk %d: %v", index, err)
		}
	}
}

func TestBackupUploadSessionRoundTrip(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	if session.ChunkCount < 2 {
		t.Fatalf("expected a multi-chunk session, got %d", session.ChunkCount)
	}
	if status, err := fixture.service.UploadStatus(t.Context(), actor, session.ID); err != nil ||
		len(status.ReceivedIndexes) != 0 {
		t.Fatalf("initial status = %+v, %v", status, err)
	}
	uploadAllChunks(t, fixture, actor, session, content)
	status, err := fixture.service.UploadStatus(t.Context(), actor, session.ID)
	if err != nil || len(status.ReceivedIndexes) != session.ChunkCount {
		t.Fatalf("received status = %+v, %v", status, err)
	}
	if status.Session.ReceivedBytes != session.DeclaredBytes {
		t.Fatalf("received bytes = %d", status.Session.ReceivedBytes)
	}
	record, err := fixture.service.FinalizeUpload(t.Context(), actor, session.ID)
	if err != nil {
		t.Fatalf("finalize upload: %v", err)
	}
	if record.State != model.BackupStateValidated || record.Source != "uploaded" {
		t.Fatalf("record = %+v", record)
	}
	if record.SizeBytes != session.DeclaredBytes {
		t.Fatalf("record size = %d", record.SizeBytes)
	}
	validated, err := fixture.service.UploadStatus(t.Context(), actor, session.ID)
	if err != nil || validated.Session.State != model.BackupUploadStateValidated ||
		validated.Session.BackupID != record.ID {
		t.Fatalf("validated session = %+v, %v", validated.Session, err)
	}
	staging, err := fixture.service.stagingPath(session.StagingName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.service.root), "restore.pending.json")); !os.IsNotExist(err) {
		t.Fatalf("finalize staged a restore: %v", err)
	}
	records, err := listBackups(t, fixture.service)
	if err != nil || len(records) != 2 {
		t.Fatalf("backup records = %d, %v", len(records), err)
	}
}

func TestBackupUploadRejectsChunkDigestAndRange(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	chunk := content[:model.MinimumBackupUploadChunkBytes]
	_, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, UploadChunkRequest{
		Index: 0, Offset: 0, SizeBytes: model.MinimumBackupUploadChunkBytes,
		Digest: hex.EncodeToString(make([]byte, 32)), Body: bytes.NewReader(chunk)})
	if !errors.Is(err, ErrUploadChunkDigest) {
		t.Fatalf("digest error = %v", err)
	}
	if _, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, UploadChunkRequest{
		Index: 0, Offset: 8, SizeBytes: model.MinimumBackupUploadChunkBytes, Digest: "a",
		Body: bytes.NewReader(chunk)}); !errors.Is(err, ErrUploadChunkRange) {
		t.Fatalf("range error = %v", err)
	}
	if _, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, UploadChunkRequest{
		Index: 0, Offset: 0, SizeBytes: model.MinimumBackupUploadChunkBytes, Digest: "a",
		Body: bytes.NewReader(chunk)}); !errors.Is(err, ErrUploadDigest) {
		t.Fatalf("invalid digest error = %v", err)
	}
	if status, err := fixture.service.UploadStatus(t.Context(), actor, session.ID); err != nil ||
		status.Session.State != model.BackupUploadStateOpen ||
		status.Session.ReceivedChunks != 0 {
		t.Fatalf("session after rejected chunks = %+v, %v", status.Session, err)
	}
	if entries, err := os.ReadDir(filepath.Join(fixture.service.root, "uploads", session.StagingName)); err != nil || len(entries) != 0 {
		t.Fatalf("staging entries = %d, %v", len(entries), err)
	}
}

func TestBackupUploadRejectsIncompleteAndConflictingUploads(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	if err := uploadChunkAtIndex(t, fixture, actor, session, content, 0); err != nil {
		t.Fatalf("upload first chunk: %v", err)
	}
	if _, err := fixture.service.FinalizeUpload(t.Context(), actor, session.ID); !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("incomplete error = %v", err)
	}
	start := model.OffsetForBackupUploadChunk(1, session.ChunkBytes)
	size := model.BackupUploadChunkSize(1, session.ChunkBytes, session.DeclaredBytes)
	conflicting := bytes.Repeat([]byte{9}, int(size))
	digest := sha256.Sum256(conflicting)
	if _, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, UploadChunkRequest{
		Index: 1, Offset: start, SizeBytes: size, Digest: hex.EncodeToString(digest[:]),
		Body: bytes.NewReader(conflicting)}); err != nil {
		t.Fatalf("upload second chunk: %v", err)
	}
	if err := uploadChunkAtIndex(t, fixture, actor, session, content, 1); !errors.Is(err, ErrUploadChunkConflict) {
		t.Fatalf("conflicting error = %v", err)
	}
	status, err := fixture.service.UploadStatus(t.Context(), actor, session.ID)
	if err != nil || status.Session.State != model.BackupUploadStateQuarantined ||
		status.Session.LastError == "" {
		t.Fatalf("quarantined session = %+v, %v", status.Session, err)
	}
	if _, err := fixture.service.FinalizeUpload(t.Context(), actor, session.ID); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("finalize quarantined error = %v", err)
	}
}

func TestBackupUploadIdempotentRetryAndOwnerIsolation(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	if err := uploadChunkAtIndex(t, fixture, actor, session, content, 0); err != nil {
		t.Fatalf("upload chunk: %v", err)
	}
	if err := uploadChunkAtIndex(t, fixture, actor, session, content, 0); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	status, err := fixture.service.UploadStatus(t.Context(), actor, session.ID)
	if err != nil || status.Session.ReceivedChunks != 1 {
		t.Fatalf("retry status = %+v, %v", status.Session, err)
	}
	intruder := store.BackupUploadActor{UserID: "owner-2"}
	if _, err := fixture.service.UploadStatus(t.Context(), intruder, session.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("intruder status error = %v", err)
	}
	if err := uploadChunkAtIndex(t, fixture, intruder, session, content, 1); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("intruder chunk error = %v", err)
	}
	if _, err := fixture.service.FinalizeUpload(t.Context(), intruder, session.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("intruder finalize error = %v", err)
	}
	if err := fixture.service.CancelUpload(t.Context(), intruder, session.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("intruder cancel error = %v", err)
	}
	if listed, err := fixture.service.ListUploads(t.Context(), intruder); err != nil || len(listed) != 0 {
		t.Fatalf("intruder list = %d, %v", len(listed), err)
	}
}

func TestBackupUploadCancelAndDeclarationValidation(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	if _, err := fixture.service.StartUpload(t.Context(), actor, UploadSessionRequest{
		Filename: "../escape.zip", SizeBytes: 4, Digest: hex.EncodeToString(make([]byte, 32))}); !errors.Is(err, ErrUploadFilename) {
		t.Fatalf("filename error = %v", err)
	}
	if _, err := fixture.service.StartUpload(t.Context(), actor, UploadSessionRequest{
		Filename: "archive.zip", SizeBytes: 4, Digest: "not-a-digest"}); !errors.Is(err, ErrUploadDigest) {
		t.Fatalf("declaration digest error = %v", err)
	}
	if _, err := fixture.service.StartUpload(t.Context(), actor, UploadSessionRequest{
		Filename: "archive.zip", SizeBytes: MaximumUploadBytes + 1,
		Digest: hex.EncodeToString(make([]byte, 32))}); !errors.Is(err, ErrUploadChunkRange) {
		t.Fatalf("size error = %v", err)
	}
	if _, err := fixture.service.UploadStatus(t.Context(), actor, "absent"); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("absent status error = %v", err)
	}
	if err := fixture.service.CancelUpload(t.Context(), actor, session.ID); err != nil {
		t.Fatalf("cancel upload: %v", err)
	}
	if err := fixture.service.CancelUpload(t.Context(), actor, session.ID); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("second cancel error = %v", err)
	}
	staging, err := fixture.service.stagingPath(session.StagingName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("cancelled staging directory still exists: %v", err)
	}
}

func TestBackupUploadResumesAfterRestart(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	if err := uploadChunkAtIndex(t, fixture, actor, session, content, 0); err != nil {
		t.Fatalf("upload chunk: %v", err)
	}
	restarted, err := NewService(t.Context(), store.NewBackupStore(fixture.sqlite.DB),
		fixture.sqlite, fixture.mts, fixture.service.root,
		filepath.Join(fixture.dataDir, "admin-data.key"), "test")
	if err != nil {
		t.Fatalf("restart backup service: %v", err)
	}
	restarted.SetUploadSessions(store.NewBackupUploadStore(fixture.sqlite.DB))
	restarted.SetSecretLifecycle(fixture.service.secretLifecycle)
	resumed, err := restarted.UploadStatus(t.Context(), actor, session.ID)
	if err != nil || len(resumed.ReceivedIndexes) != 1 {
		t.Fatalf("resumed status = %+v, %v", resumed, err)
	}
	for index := 1; index < session.ChunkCount; index++ {
		if err := uploadChunkAtIndex(t, fixture, actor, session, content, index); err != nil {
			t.Fatalf("upload chunk %d: %v", index, err)
		}
	}
	record, err := restarted.FinalizeUpload(t.Context(), actor, session.ID)
	if err != nil || record.State != model.BackupStateValidated {
		t.Fatalf("finalize after restart = %+v, %v", record, err)
	}
}

func TestBackupUploadMaintenanceExpiresAndPrunes(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)
	session, actor := startUpload(t, fixture, content)
	if _, err := fixture.sqlite.DB.Exec(`UPDATE backup_upload_sessions SET expires_at = ?
		WHERE id = ?`, time.Now().UTC().Add(-time.Minute), session.ID); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := fixture.service.UploadChunk(t.Context(), actor, session.ID, UploadChunkRequest{
		Index: 0, Offset: 0, SizeBytes: 1, Digest: hex.EncodeToString(make([]byte, 32)),
		Body: bytes.NewReader([]byte{1})}); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("expired chunk error = %v", err)
	}
	if err := fixture.service.MaintainUploadSessions(t.Context()); err != nil {
		t.Fatalf("maintain uploads: %v", err)
	}
	status, err := fixture.service.UploadStatus(t.Context(), actor, session.ID)
	if err != nil || status.Session.State != model.BackupUploadStateExpired {
		t.Fatalf("expired session = %+v, %v", status.Session, err)
	}
	staging, err := fixture.service.stagingPath(session.StagingName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("expired staging directory still exists: %v", err)
	}
	for index := 0; index < 40; index++ {
		created, err := fixture.service.StartUpload(t.Context(), actor, UploadSessionRequest{
			Filename: "restore.zip", SizeBytes: 16,
			Digest: hex.EncodeToString(make([]byte, 32))})
		if err != nil {
			t.Fatalf("start upload %d: %v", index, err)
		}
		if err := fixture.service.CancelUpload(t.Context(), actor, created.Session.ID); err != nil {
			t.Fatalf("cancel upload %d: %v", index, err)
		}
	}
	if err := fixture.service.MaintainUploadSessions(t.Context()); err != nil {
		t.Fatalf("maintain uploads: %v", err)
	}
	var retained int
	if err := fixture.sqlite.DB.QueryRow(`SELECT COUNT(*) FROM backup_upload_sessions
		WHERE owner_user_id = ?`, uploadTestOwner).Scan(&retained); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if retained > model.MaximumBackupUploadsPerOwner {
		t.Fatalf("retained sessions = %d", retained)
	}
}

func TestBackupUploadUnavailableWithoutStore(t *testing.T) {
	fixture := newBackupFixture(t)
	if _, err := fixture.service.StartUpload(t.Context(), uploadActor(), UploadSessionRequest{
		Filename: "restore.zip", SizeBytes: 4, Digest: hex.EncodeToString(make([]byte, 32))}); !errors.Is(err, ErrUploadUnavailable) {
		t.Fatalf("unavailable start error = %v", err)
	}
	if _, err := fixture.service.ListUploads(t.Context(), uploadActor()); !errors.Is(err, ErrUploadUnavailable) {
		t.Fatalf("unavailable list error = %v", err)
	}
	if err := fixture.service.MaintainUploadSessions(context.Background()); err != nil {
		t.Fatalf("maintenance without store: %v", err)
	}
}
