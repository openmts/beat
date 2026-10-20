package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestFinalizeUploadQuarantinePaths(t *testing.T) {
	fixture := newUploadFixture(t)
	content := largeArchive(t, fixture)

	digestCase, actor := startUpload(t, fixture, content)
	uploadAllChunks(t, fixture, actor, digestCase, content)
	if _, err := fixture.sqlite.DB.Exec(`UPDATE backup_upload_sessions SET declared_digest=? WHERE id=?`,
		sha256Hex([]byte("different content")), digestCase.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.FinalizeUpload(t.Context(), actor, digestCase.ID); !errors.Is(err, ErrUploadChunkDigest) {
		t.Fatalf("digest mismatch error = %v", err)
	}
	assertQuarantined(t, fixture, digestCase.ID)

	missingCase, actor := startUpload(t, fixture, content)
	uploadAllChunks(t, fixture, actor, missingCase, content)
	staging, err := fixture.service.stagingPath(missingCase.StagingName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(chunkFilePath(staging, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.FinalizeUpload(t.Context(), actor, missingCase.ID); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("missing chunk error = %v", err)
	}
	assertQuarantined(t, fixture, missingCase.ID)

	nameCase, actor := startUpload(t, fixture, content)
	uploadAllChunks(t, fixture, actor, nameCase, content)
	if _, err := fixture.sqlite.DB.Exec(`UPDATE backup_upload_sessions SET staging_name='bad/name' WHERE id=?`,
		nameCase.ID); err != nil {
		t.Fatal(err)
	}
	// 暂存目录名非法时在装配前直接失败，此时还没有产生任何已写出的归档。
	if _, err := fixture.service.FinalizeUpload(t.Context(), actor, nameCase.ID); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("bad staging name error = %v", err)
	}
	status, err := fixture.service.UploadStatus(t.Context(), actor, nameCase.ID)
	if err != nil || status.Session.State != model.BackupUploadStateOpen {
		t.Fatalf("bad staging session = %+v / %v", status.Session, err)
	}
}

func assertQuarantined(t *testing.T, fixture *backupFixture, id string) {
	t.Helper()
	status, err := fixture.service.UploadStatus(t.Context(), uploadActor(), id)
	if err != nil || status.Session.State != model.BackupUploadStateQuarantined {
		t.Fatalf("quarantine state = %+v / %v", status.Session, err)
	}
}

func TestCheckUploadCompletenessBranches(t *testing.T) {
	size := int64(model.MinimumBackupUploadChunkBytes) * 3
	chunkBytes, chunkCount, ok := model.BackupUploadGeometry(size)
	if !ok {
		t.Fatal("geometry rejected")
	}
	session := &model.BackupUploadSession{DeclaredBytes: size, ChunkBytes: chunkBytes, ChunkCount: chunkCount}
	complete := make([]model.BackupUploadChunk, 0, chunkCount)
	for index := 0; index < chunkCount; index++ {
		complete = append(complete, model.BackupUploadChunk{Index: index,
			Offset:    model.OffsetForBackupUploadChunk(index, chunkBytes),
			SizeBytes: model.BackupUploadChunkSize(index, chunkBytes, size)})
	}
	if err := checkUploadCompleteness(session, complete); err != nil {
		t.Fatalf("complete upload error = %v", err)
	}
	if err := checkUploadCompleteness(session, complete[:len(complete)-1]); !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("missing chunk error = %v", err)
	}
	shifted := append([]model.BackupUploadChunk{}, complete...)
	shifted[1].Index = 7
	if err := checkUploadCompleteness(session, shifted); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("index error = %v", err)
	}
	wrongSize := append([]model.BackupUploadChunk{}, complete...)
	wrongSize[1].SizeBytes++
	if err := checkUploadCompleteness(session, wrongSize); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("size error = %v", err)
	}
	wrongOffset := append([]model.BackupUploadChunk{}, complete...)
	wrongOffset[1].Offset++
	if err := checkUploadCompleteness(session, wrongOffset); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("offset error = %v", err)
	}
	// ChunkCount 少于实际所需分块时，逐块校验都通过但总和不足。
	truncated := &model.BackupUploadSession{DeclaredBytes: size, ChunkBytes: chunkBytes, ChunkCount: chunkCount - 1}
	if err := checkUploadCompleteness(truncated, complete[:chunkCount-1]); !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("total error = %v", err)
	}
}

func TestCopyUploadChunkBranches(t *testing.T) {
	staging := t.TempDir()
	payload := []byte("chunk payload")
	chunk := model.BackupUploadChunk{Index: 3, SizeBytes: int64(len(payload)), Digest: sha256Hex(payload)}
	if _, err := copyUploadChunk(&bytes.Buffer{}, staging, chunk); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("missing chunk error = %v", err)
	}
	if err := os.Mkdir(chunkFilePath(staging, chunk.Index), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := copyUploadChunk(&bytes.Buffer{}, staging, chunk); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("directory chunk error = %v", err)
	}
	if err := os.Remove(chunkFilePath(staging, chunk.Index)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkFilePath(staging, chunk.Index), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	smaller := chunk
	smaller.SizeBytes++
	if _, err := copyUploadChunk(&bytes.Buffer{}, staging, smaller); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("size mismatch error = %v", err)
	}
	corrupt := chunk
	corrupt.Digest = sha256Hex([]byte("other"))
	if _, err := copyUploadChunk(&bytes.Buffer{}, staging, corrupt); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("digest mismatch error = %v", err)
	}
	var destination bytes.Buffer
	written, err := copyUploadChunk(&destination, staging, chunk)
	if err != nil || written != int64(len(payload)) || destination.String() != string(payload) {
		t.Fatalf("copy = %d / %q / %v", written, destination.String(), err)
	}
}

func TestWriteChunkFileFailureBranches(t *testing.T) {
	fixture := newUploadFixture(t)
	staging := t.TempDir()
	payload := []byte("payload")
	request := func(body []byte, digest string) UploadChunkRequest {
		return UploadChunkRequest{Index: 1, SizeBytes: int64(len(payload)), Digest: digest,
			Body: bytes.NewReader(body)}
	}
	if _, _, err := fixture.service.writeChunkFile(staging, request(payload, sha256Hex(payload))); err != nil {
		t.Fatalf("write chunk: %v", err)
	}
	if err := os.Remove(chunkFilePath(staging, 1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.service.writeChunkFile(staging, request(payload[:2], sha256Hex(payload))); !errors.Is(err, ErrUploadChunkRange) {
		t.Fatalf("short body error = %v", err)
	}
	if _, _, err := fixture.service.writeChunkFile(staging, request(payload, sha256Hex([]byte("other")))); !errors.Is(err, ErrUploadChunkDigest) {
		t.Fatalf("digest error = %v", err)
	}
	if _, _, err := fixture.service.writeChunkFile(filepath.Join(staging, "absent"),
		request(payload, sha256Hex(payload))); err == nil {
		t.Fatal("missing staging must fail")
	}
	blocked := filepath.Join(staging, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.service.writeChunkFile(blocked, request(payload, sha256Hex(payload))); err == nil {
		t.Fatal("file staging must fail")
	}
	final := chunkFilePath(staging, 1)
	if err := os.Mkdir(final, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(final, "busy"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.service.writeChunkFile(staging, request(payload, sha256Hex(payload))); err == nil {
		t.Fatal("occupied final path must fail")
	}
}

func TestUploadHelperValidation(t *testing.T) {
	cases := map[string]error{
		"":                   ErrUploadFilename,
		"../escape.zip":      ErrUploadFilename,
		"nested/archive.zip": ErrUploadFilename,
		"bad\x01name.zip":    ErrUploadFilename,
		string(make([]byte, model.MaximumBackupUploadFilenameBytes+1)): ErrUploadFilename,
	}
	for name, want := range cases {
		if err := validateUploadFilename(name); !errors.Is(err, want) {
			t.Fatalf("filename %q error = %v", name, err)
		}
	}
	if err := validateUploadFilename("archive.zip"); err != nil {
		t.Fatalf("valid filename error = %v", err)
	}
	if validDigest("") || validDigest(sha256Hex([]byte("x"))+"0") || validDigest("AA"+sha256Hex([]byte("x"))[2:]) {
		t.Fatal("digest validation accepted invalid input")
	}
	if !validDigest(sha256Hex([]byte("x"))) {
		t.Fatal("valid digest rejected")
	}
	if err := mapUploadStoreError(store.ErrBackupUploadLimit); !errors.Is(err, ErrUploadLimit) {
		t.Fatalf("limit mapping = %v", err)
	}
	if err := mapUploadStoreError(store.ErrBackupUploadClosed); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("closed mapping = %v", err)
	}
	if err := mapUploadStoreError(errUploadFault); !errors.Is(err, errUploadFault) {
		t.Fatalf("generic mapping = %v", err)
	}
	if err := mapUploadChunkError(store.ErrBackupUploadChunkConflict); !errors.Is(err, ErrUploadChunkConflict) {
		t.Fatalf("conflict mapping = %v", err)
	}
	if err := mapUploadChunkError(store.ErrBackupUploadState); !errors.Is(err, ErrUploadClosed) {
		t.Fatalf("state mapping = %v", err)
	}
	if err := mapUploadChunkError(errUploadFault); !errors.Is(err, errUploadFault) {
		t.Fatalf("generic chunk mapping = %v", err)
	}
	fixture := newUploadFixture(t)
	if _, err := fixture.service.stagingPath("bad/name"); !errors.Is(err, ErrUploadStaging) {
		t.Fatalf("staging path error = %v", err)
	}
	fixture.service.removeStaging("bad/name")
	if root := fixture.service.uploadRoot(); filepath.Base(root) != "uploads" {
		t.Fatalf("upload root = %s", root)
	}
	if validThemeBackupPath("themes/x/archive.zip") || !validThemeBackupPath("themes/"+
		sha256Hex([]byte("x"))+"/assets/app.css") || validThemeBackupPath("themes/"+
		sha256Hex([]byte("x"))+"/../escape") {
		t.Fatal("theme backup path validation is wrong")
	}
}
