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

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// UploadChunkRequest 描述一次分块提交：索引、精确字节范围与摘要。
type UploadChunkRequest struct {
	Index     int
	Offset    int64
	SizeBytes int64
	Digest    string
	Body      io.Reader
}

// UploadChunk 校验并持久化一个分块，重复提交相同字节是幂等的。
func (service *Service) UploadChunk(
	ctx context.Context,
	actor store.BackupUploadActor,
	id string,
	request UploadChunkRequest,
) (model.BackupUploadChunk, error) {
	session, err := service.ownedUpload(ctx, actor, id)
	if err != nil {
		return model.BackupUploadChunk{}, err
	}
	if session.State != model.BackupUploadStateOpen {
		return model.BackupUploadChunk{}, ErrUploadClosed
	}
	if !session.ExpiresAt.After(service.now()) {
		if _, err := service.uploads.Expire(ctx, service.now()); err != nil {
			return model.BackupUploadChunk{}, err
		}
		return model.BackupUploadChunk{}, ErrUploadClosed
	}
	expected := model.BackupUploadChunkSize(request.Index, session.ChunkBytes, session.DeclaredBytes)
	if expected <= 0 || request.SizeBytes != expected ||
		request.Offset != model.OffsetForBackupUploadChunk(request.Index, session.ChunkBytes) {
		return model.BackupUploadChunk{}, ErrUploadChunkRange
	}
	if !validDigest(request.Digest) {
		return model.BackupUploadChunk{}, ErrUploadDigest
	}
	staging, err := service.stagingPath(session.StagingName)
	if err != nil {
		return model.BackupUploadChunk{}, err
	}
	digest, path, err := service.writeChunkFile(staging, request)
	if err != nil {
		return model.BackupUploadChunk{}, err
	}
	existing, err := service.uploads.Chunk(ctx, session.ID, request.Index)
	if err != nil {
		_ = os.Remove(path)
		return model.BackupUploadChunk{}, err
	}
	if existing != nil {
		_ = os.Remove(path)
		if existing.Digest == digest && existing.SizeBytes == request.SizeBytes {
			return *existing, nil
		}
		service.quarantine(ctx, actor, session, "chunk conflicts with an earlier attempt")
		return model.BackupUploadChunk{}, ErrUploadChunkConflict
	}
	chunk := model.BackupUploadChunk{SessionID: session.ID, Index: request.Index,
		Offset: request.Offset, SizeBytes: request.SizeBytes, Digest: digest,
		CreatedAt: service.now()}
	stored, _, err := service.uploads.AppendChunk(ctx, store.BackupUploadChunkAppend{
		Actor: actor, Session: *session, Chunk: chunk, Now: chunk.CreatedAt})
	if err != nil {
		_ = os.Remove(path)
		return model.BackupUploadChunk{}, mapUploadChunkError(err)
	}
	return stored, nil
}

// writeChunkFile 把请求体写入私有暂存目录，返回实际摘要与最终路径。
func (service *Service) writeChunkFile(
	staging string,
	request UploadChunkRequest,
) (string, string, error) {
	if err := os.Chmod(staging, 0o700); err != nil {
		return "", "", fmt.Errorf("secure backup upload staging: %w", err)
	}
	temporary := chunkFilePath(staging, request.Index) + ".part"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("create backup upload chunk: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash),
		io.LimitReader(request.Body, request.SizeBytes+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temporary)
		return "", "", fmt.Errorf("write backup upload chunk: %w", err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if written != request.SizeBytes {
		_ = os.Remove(temporary)
		return "", "", ErrUploadChunkRange
	}
	if digest != request.Digest {
		_ = os.Remove(temporary)
		return "", "", ErrUploadChunkDigest
	}
	final := chunkFilePath(staging, request.Index)
	if err := os.Rename(temporary, final); err != nil {
		_ = os.Remove(temporary)
		return "", "", fmt.Errorf("publish backup upload chunk: %w", err)
	}
	return digest, final, nil
}

func chunkFilePath(staging string, index int) string {
	return filepath.Join(staging, fmt.Sprintf("chunk-%08d", index))
}

func mapUploadChunkError(err error) error {
	switch {
	case errors.Is(err, store.ErrBackupUploadChunkConflict):
		return ErrUploadChunkConflict
	case errors.Is(err, store.ErrBackupUploadState), errors.Is(err, store.ErrBackupUploadClosed):
		return ErrUploadClosed
	default:
		return err
	}
}
