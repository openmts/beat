package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// FinalizeUpload 组装、校验并把上传登记为 validated 备份记录。
//
// 校验成功也只是 validated，绝不自动 stage 或 apply 恢复。
func (service *Service) FinalizeUpload(
	ctx context.Context,
	actor store.BackupUploadActor,
	id string,
) (model.BackupRecord, error) {
	service.uploadMu.Lock()
	defer service.uploadMu.Unlock()
	session, err := service.ownedUpload(ctx, actor, id)
	if err != nil {
		return model.BackupRecord{}, err
	}
	if session.State != model.BackupUploadStateOpen {
		return model.BackupRecord{}, ErrUploadClosed
	}
	chunks, err := service.uploads.ListChunks(ctx, id)
	if err != nil {
		return model.BackupRecord{}, err
	}
	if err := checkUploadCompleteness(session, chunks); err != nil {
		return model.BackupRecord{}, err
	}
	staging, err := service.stagingPath(session.StagingName)
	if err != nil {
		return model.BackupRecord{}, err
	}
	temporary, size, digest, err := service.assembleArchive(session, chunks, staging)
	if err != nil {
		if errors.Is(err, ErrUploadStaging) {
			service.quarantine(ctx, actor, session, err.Error())
		}
		return model.BackupRecord{}, err
	}
	if size != session.DeclaredBytes || digest != session.DeclaredDigest {
		_ = os.Remove(temporary)
		service.quarantine(ctx, actor, session, "archive digest does not match the declaration")
		return model.BackupRecord{}, ErrUploadChunkDigest
	}
	return service.publishUpload(ctx, actor, session, temporary, size)
}

func (service *Service) assembleArchive(
	session *model.BackupUploadSession,
	chunks []model.BackupUploadChunk,
	staging string,
) (string, int64, string, error) {
	file, err := os.CreateTemp(service.root, ".upload-")
	if err != nil {
		return "", 0, "", fmt.Errorf("create uploaded archive: %w", err)
	}
	temporary := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(temporary)
		return "", 0, "", fmt.Errorf("secure uploaded archive: %w", err)
	}
	hash := sha256.New()
	size, copyErr := copyUploadChunks(io.MultiWriter(file, hash), chunks, staging)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temporary)
		return "", 0, "", err
	}
	return temporary, size, hex.EncodeToString(hash.Sum(nil)), nil
}

func copyUploadChunks(
	destination io.Writer,
	chunks []model.BackupUploadChunk,
	staging string,
) (int64, error) {
	var total int64
	for _, chunk := range chunks {
		written, err := copyUploadChunk(destination, staging, chunk)
		if err != nil {
			return total, err
		}
		total += written
	}
	return total, nil
}

func copyUploadChunk(
	destination io.Writer,
	staging string,
	chunk model.BackupUploadChunk,
) (int64, error) {
	path := chunkFilePath(staging, chunk.Index)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != chunk.SizeBytes {
		return 0, ErrUploadStaging
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, ErrUploadStaging
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destination, hash), file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return 0, fmt.Errorf("read uploaded chunk: %w", err)
	}
	if written != chunk.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != chunk.Digest {
		return 0, ErrUploadStaging
	}
	return written, nil
}

func (service *Service) publishUpload(
	ctx context.Context,
	actor store.BackupUploadActor,
	session *model.BackupUploadSession,
	temporary string,
	size int64,
) (model.BackupRecord, error) {
	name := fmt.Sprintf("beat-upload-v1-%s.zip", uuid.NewString())
	path := filepath.Join(service.root, name)
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return model.BackupRecord{}, fmt.Errorf("publish uploaded archive: %w", err)
	}
	validated, err := validateArchive(ctx, path, service.root)
	if err != nil {
		_ = os.Remove(path)
		service.quarantine(ctx, actor, session, err.Error())
		return model.BackupRecord{}, err
	}
	defer func() { _ = os.RemoveAll(validated.root) }()
	completed := service.now()
	record := model.BackupRecord{ID: uuid.NewString(), Filename: name, Source: "uploaded",
		State: model.BackupStateValidated, CreatedAt: completed, CompletedAt: &completed,
		SnapshotCutoff: &validated.manifest.SnapshotCutoff, SizeBytes: size,
		SQLiteBytes:  validated.manifest.PayloadSizes[sqliteEntry],
		MetricsBytes: validated.manifest.PayloadSizes[metricsEntry],
		MetricRows:   validated.manifest.MetricRows}
	if err := service.records.Create(ctx, &record); err != nil {
		_ = os.Remove(path)
		return model.BackupRecord{}, err
	}
	if err := service.uploads.MarkValidated(ctx, actor, session.ID, record.ID, service.now()); err != nil {
		_ = os.Remove(path)
		_ = service.records.Delete(ctx, record.ID)
		return model.BackupRecord{}, mapUploadStoreError(err)
	}
	service.discardStaging(session)
	return record, nil
}

func checkUploadCompleteness(
	session *model.BackupUploadSession,
	chunks []model.BackupUploadChunk,
) error {
	if len(chunks) != session.ChunkCount {
		return ErrUploadIncomplete
	}
	var total int64
	for position, chunk := range chunks {
		expected := model.BackupUploadChunkSize(position, session.ChunkBytes, session.DeclaredBytes)
		if chunk.Index != position || chunk.SizeBytes != expected ||
			chunk.Offset != model.OffsetForBackupUploadChunk(position, session.ChunkBytes) ||
			total+chunk.SizeBytes > session.DeclaredBytes {
			return ErrUploadStaging
		}
		total += chunk.SizeBytes
	}
	if total != session.DeclaredBytes {
		return ErrUploadIncomplete
	}
	return nil
}

func (service *Service) quarantine(
	ctx context.Context,
	actor store.BackupUploadActor,
	session *model.BackupUploadSession,
	reason string,
) {
	if err := service.uploads.SetState(ctx, actor, session.ID,
		model.BackupUploadStateQuarantined, reason, service.now()); err != nil {
		_ = err
	}
}
