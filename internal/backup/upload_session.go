package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// BackupUploadSessionTTL 是分块上传会话的有效期。
const BackupUploadSessionTTL = 24 * time.Hour

var (
	ErrUploadUnavailable   = errors.New("resumable backup uploads are unavailable")
	ErrUploadLimit         = errors.New("too many open backup upload sessions")
	ErrUploadNotFound      = errors.New("backup upload session was not found")
	ErrUploadClosed        = errors.New("backup upload session is no longer open")
	ErrUploadFilename      = errors.New("backup upload filename is invalid")
	ErrUploadDigest        = errors.New("backup upload digest is invalid")
	ErrUploadChunkRange    = errors.New("backup upload chunk range is invalid")
	ErrUploadChunkDigest   = errors.New("backup upload chunk digest does not match the bytes")
	ErrUploadChunkConflict = errors.New("backup upload chunk conflicts with an earlier attempt")
	ErrUploadIncomplete    = errors.New("backup upload is missing chunks")
	ErrUploadStaging       = errors.New("backup upload staging state is inconsistent")
)

// UploadSessionStatus 是会话进度视图：会话本身加上已收到的分块索引。
type UploadSessionStatus struct {
	Session         model.BackupUploadSession
	ReceivedIndexes []int
}

// UploadSessionRequest 描述客户端声明的归档信息。
type UploadSessionRequest struct {
	Filename  string
	SizeBytes int64
	Digest    string
}

type UploadSessionStore interface {
	Create(context.Context, store.BackupUploadActor, *model.BackupUploadSession) error
	Get(context.Context, string) (*model.BackupUploadSession, error)
	ListByOwner(context.Context, string, int) ([]model.BackupUploadSession, error)
	ListQuarantined(context.Context, int) ([]model.BackupUploadSession, error)
	SetState(context.Context, store.BackupUploadActor, string, string, string, time.Time) error
	MarkValidated(context.Context, store.BackupUploadActor, string, string, time.Time) error
	ListChunks(context.Context, string) ([]model.BackupUploadChunk, error)
	Chunk(context.Context, string, int) (*model.BackupUploadChunk, error)
	AppendChunk(context.Context, store.BackupUploadChunkAppend) (model.BackupUploadChunk, bool, error)
	Expire(context.Context, time.Time) (int, error)
	PendingStagingCleanup(context.Context, int) ([]model.BackupUploadSession, error)
	MarkStagingRemoved(context.Context, string, time.Time) error
	Prune(context.Context, int, int) (int, error)
}

// SetUploadSessions 注入可分块上传的持久化实现。
func (service *Service) SetUploadSessions(uploads UploadSessionStore) {
	service.uploads = uploads
}

func (service *Service) StartUpload(
	ctx context.Context,
	actor store.BackupUploadActor,
	request UploadSessionRequest,
) (UploadSessionStatus, error) {
	if service.uploads == nil {
		return UploadSessionStatus{}, ErrUploadUnavailable
	}
	if err := validateUploadFilename(request.Filename); err != nil {
		return UploadSessionStatus{}, err
	}
	if !validDigest(request.Digest) {
		return UploadSessionStatus{}, ErrUploadDigest
	}
	chunkBytes, chunkCount, ok := model.BackupUploadGeometry(request.SizeBytes)
	if !ok {
		return UploadSessionStatus{}, ErrUploadChunkRange
	}
	now := service.now()
	session := model.BackupUploadSession{ID: uuid.NewString(), OwnerUserID: actor.UserID,
		Filename: request.Filename, DeclaredBytes: request.SizeBytes, DeclaredDigest: request.Digest,
		ChunkBytes: chunkBytes, ChunkCount: chunkCount, StagingName: "u-" + uuid.NewString(),
		State: model.BackupUploadStateOpen, ExpiresAt: now.Add(BackupUploadSessionTTL),
		CreatedAt: now, UpdatedAt: now}
	if err := service.prepareStaging(session.StagingName); err != nil {
		return UploadSessionStatus{}, err
	}
	if err := service.uploads.Create(ctx, actor, &session); err != nil {
		service.removeStaging(session.StagingName)
		if errors.Is(err, store.ErrBackupUploadLimit) {
			return UploadSessionStatus{}, ErrUploadLimit
		}
		return UploadSessionStatus{}, err
	}
	_ = service.MaintainUploadSessions(ctx)
	return UploadSessionStatus{Session: session, ReceivedIndexes: []int{}}, nil
}

// ListUploads 返回该管理员最近的会话历史。
func (service *Service) ListUploads(
	ctx context.Context,
	actor store.BackupUploadActor,
) ([]model.BackupUploadSession, error) {
	if service.uploads == nil {
		return nil, ErrUploadUnavailable
	}
	return service.uploads.ListByOwner(ctx, actor.UserID, model.MaximumBackupUploadsPerOwner)
}

// UploadStatus 返回单个会话的进度；会话不属于调用方时按不存在处理。
func (service *Service) UploadStatus(
	ctx context.Context,
	actor store.BackupUploadActor,
	id string,
) (UploadSessionStatus, error) {
	session, err := service.ownedUpload(ctx, actor, id)
	if err != nil {
		return UploadSessionStatus{}, err
	}
	chunks, err := service.uploads.ListChunks(ctx, session.ID)
	if err != nil {
		return UploadSessionStatus{}, err
	}
	indexes := make([]int, 0, len(chunks))
	for _, chunk := range chunks {
		indexes = append(indexes, chunk.Index)
	}
	return UploadSessionStatus{Session: *session, ReceivedIndexes: indexes}, nil
}

// CancelUpload 丢弃未完成的会话并删除它的私有暂存目录。
func (service *Service) CancelUpload(
	ctx context.Context,
	actor store.BackupUploadActor,
	id string,
) error {
	session, err := service.ownedUpload(ctx, actor, id)
	if err != nil {
		return err
	}
	if session.State != model.BackupUploadStateOpen {
		return ErrUploadClosed
	}
	if err := service.uploads.SetState(ctx, actor, id, model.BackupUploadStateCancelled,
		"cancelled by owner", service.now()); err != nil {
		return mapUploadStoreError(err)
	}
	service.discardStaging(session)
	return nil
}

func (service *Service) ownedUpload(
	ctx context.Context,
	actor store.BackupUploadActor,
	id string,
) (*model.BackupUploadSession, error) {
	if service.uploads == nil {
		return nil, ErrUploadUnavailable
	}
	if id == "" || actor.UserID == "" {
		return nil, ErrUploadNotFound
	}
	session, err := service.uploads.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if session == nil || session.OwnerUserID != actor.UserID {
		return nil, ErrUploadNotFound
	}
	return session, nil
}

func validateUploadFilename(filename string) error {
	if filename == "" || len(filename) > model.MaximumBackupUploadFilenameBytes {
		return ErrUploadFilename
	}
	if filepath.Base(filename) != filename || strings.ContainsAny(filename, "/\\") {
		return ErrUploadFilename
	}
	for _, character := range filename {
		if character < 0x20 || character == 0x7f {
			return ErrUploadFilename
		}
	}
	return nil
}

func validDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for _, character := range digest {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}

func (service *Service) uploadRoot() string {
	return filepath.Join(service.root, "uploads")
}

func (service *Service) stagingPath(stagingName string) (string, error) {
	if stagingName == "" || filepath.Base(stagingName) != stagingName {
		return "", ErrUploadStaging
	}
	return filepath.Join(service.root, "uploads", stagingName), nil
}

func (service *Service) prepareStaging(stagingName string) error {
	if err := os.MkdirAll(service.uploadRoot(), 0o700); err != nil {
		return fmt.Errorf("create backup upload root: %w", err)
	}
	if err := os.Chmod(service.uploadRoot(), 0o700); err != nil {
		return fmt.Errorf("secure backup upload root: %w", err)
	}
	path, err := service.stagingPath(stagingName)
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create backup upload staging: %w", err)
	}
	return nil
}

// removeStaging 只删除服务生成的精确会话目录，用于会话行尚不存在时的回滚。
func (service *Service) removeStaging(stagingName string) {
	path, err := service.stagingPath(stagingName)
	if err != nil {
		return
	}
	_ = os.RemoveAll(path)
}

// discardStaging 删除精确会话目录并在账本中记录已清理。
func (service *Service) discardStaging(session *model.BackupUploadSession) {
	service.removeStaging(session.StagingName)
	if service.uploads == nil {
		return
	}
	if err := service.uploads.MarkStagingRemoved(service.ctx, session.ID, service.now()); err != nil {
		slog.WarnContext(service.ctx, "backup upload staging cleanup failed", "session_id", session.ID, "error", err)
	}
}

func mapUploadStoreError(err error) error {
	switch {
	case errors.Is(err, store.ErrBackupUploadLimit):
		return ErrUploadLimit
	case errors.Is(err, store.ErrBackupUploadState), errors.Is(err, store.ErrBackupUploadClosed):
		return ErrUploadClosed
	default:
		return err
	}
}
