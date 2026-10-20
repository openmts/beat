package model

import "time"

// v18 分块可续传备份上传的会话状态。
const (
	BackupUploadStateOpen        = "open"
	BackupUploadStateQuarantined = "quarantined"
	BackupUploadStateValidated   = "validated"
	BackupUploadStateCancelled   = "cancelled"
	BackupUploadStateExpired     = "expired"
)

// 分块上传的边界：块大小、块数量、每个管理员同时打开的会话数与历史会话数。
const (
	MinimumBackupUploadChunkBytes    = 1 << 20
	MaximumBackupUploadChunkBytes    = 64 << 20
	MaximumBackupUploadChunks        = 4096
	MaximumOpenBackupUploadsPerOwner = 4
	MaximumBackupUploadsPerOwner     = 32
	MaximumBackupUploadFilenameBytes = 200
	MaximumBackupUploadArchiveBytes  = int64(4) << 30

	// MaximumParallelBackupUploadChunks 是服务端允许客户端并发提交的分块数。
	MaximumParallelBackupUploadChunks = 4
)

// BackupUploadSession 记录一次可分块续传的备份上传：声明信息、进度、状态与私有暂存目录名。
type BackupUploadSession struct {
	ID             string    `json:"id"`
	OwnerUserID    string    `json:"owner_user_id"`
	Filename       string    `json:"filename"`
	DeclaredBytes  int64     `json:"declared_bytes"`
	DeclaredDigest string    `json:"declared_digest"`
	ChunkBytes     int64     `json:"chunk_bytes"`
	ChunkCount     int       `json:"chunk_count"`
	ReceivedBytes  int64     `json:"received_bytes"`
	ReceivedChunks int       `json:"received_chunks"`
	StagingName    string    `json:"-"`
	StagingRemoved bool      `json:"-"`
	State          string    `json:"state"`
	BackupID       string    `json:"backup_id,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BackupUploadChunk 记录一个已提交分块的索引、字节范围与摘要。
type BackupUploadChunk struct {
	SessionID string    `json:"session_id"`
	Index     int       `json:"index"`
	Offset    int64     `json:"offset"`
	SizeBytes int64     `json:"size_bytes"`
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"created_at"`
}

// OffsetForBackupUploadChunk 返回该索引在归档中的起始偏移。
func OffsetForBackupUploadChunk(index int, chunkBytes int64) int64 {
	if index < 0 || chunkBytes <= 0 {
		return -1
	}
	return int64(index) * chunkBytes
}

// MaximumBackupUploadChunkIndex 是最后一个合法分块索引。
const MaximumBackupUploadChunkIndex = MaximumBackupUploadChunks - 1

// BackupUploadGeometry 计算归档大小对应的固定块大小与块数量。
//
// 块大小为覆盖全部数据的最小 1 MiB 对齐值，并夹在上限之间；大小非法或超出
// 分块上限时返回 false。
func BackupUploadGeometry(sizeBytes int64) (int64, int, bool) {
	if sizeBytes < 0 || sizeBytes > MaximumBackupUploadArchiveBytes {
		return 0, 0, false
	}
	if sizeBytes == 0 {
		return MinimumBackupUploadChunkBytes, 1, true
	}
	chunkBytes := (sizeBytes + MaximumBackupUploadChunks - 1) / MaximumBackupUploadChunks
	if chunkBytes < MinimumBackupUploadChunkBytes {
		chunkBytes = MinimumBackupUploadChunkBytes
	}
	if chunkBytes > MaximumBackupUploadChunkBytes {
		chunkBytes = MaximumBackupUploadChunkBytes
	}
	count := int((sizeBytes + chunkBytes - 1) / chunkBytes)
	if count < 1 || count > MaximumBackupUploadChunks {
		return 0, 0, false
	}
	return chunkBytes, count, true
}

// BackupUploadChunkSize 返回索引对应分块的实际字节数。
func BackupUploadChunkSize(index int, chunkBytes, declaredBytes int64) int64 {
	if index < 0 || index >= MaximumBackupUploadChunks || chunkBytes <= 0 || declaredBytes < 0 {
		return -1
	}
	offset := int64(index) * chunkBytes
	if offset >= declaredBytes {
		return 0
	}
	remaining := declaredBytes - offset
	if remaining < chunkBytes {
		return remaining
	}
	return chunkBytes
}
