package agentoperations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

var ErrFileIntegrity = errors.New("operation file integrity check failed")

var ErrFileConflict = errors.New("operation file transfer conflicted")

var ErrSourceChanged = errors.New("operation source file changed")

type transferProgress struct {
	StartedAt         int64                `json:"started_at"`
	DeadlineAt        int64                `json:"deadline_at"`
	Temporary         string               `json:"temporary,omitempty"`
	TemporaryIdentity string               `json:"temporary_identity,omitempty"`
	ChunkDigests      []string             `json:"chunk_digests,omitempty"`
	Phase             string               `json:"phase"`
	Source            *protocol.SourceInfo `json:"source,omitempty"`
	SourceVersion     string               `json:"source_version,omitempty"`
}

type FileGateway interface {
	DeclareSource(context.Context, protocol.Lease, protocol.SourceInfo) (protocol.FileTransferReceipt, error)
	DownloadChunk(context.Context, protocol.Lease, int) ([]byte, error)
	UploadChunk(context.Context, protocol.Lease, FileChunk) error
	FinalizeFile(context.Context, protocol.Lease) error
}

type FileChunk struct {
	Index int
	Total int64
	Data  []byte
}

func isFileLease(lease protocol.Lease) bool {
	return lease.Payload.Limits.Kind == "deploy" || lease.Payload.Limits.Kind == "collect"
}

func newTransferProgress(lease protocol.Lease) *transferProgress {
	now := time.Now().UTC()
	return &transferProgress{StartedAt: now.UnixMilli(), DeadlineAt: now.Add(time.Duration(lease.Payload.Limits.TimeoutSeconds) * time.Second).UnixMilli(), Phase: "pending"}
}

func temporaryName() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", ErrStorage
	}
	return ".beat-transfer-" + hex.EncodeToString(value), nil
}

func validTemporaryName(name string) bool {
	if !strings.HasPrefix(name, ".beat-transfer-") || len(name) != len(".beat-transfer-")+32 {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(name, ".beat-transfer-"))
	return err == nil && len(decoded) == 16
}

func validateTransferProgress(progress *transferProgress) error {
	if progress == nil {
		return nil
	}
	if !validTransferTiming(progress) || !validSourceProgress(progress) {
		return ErrStorage
	}
	if progress.Temporary != "" && !validTemporaryName(progress.Temporary) {
		return ErrStorage
	}
	if len(progress.ChunkDigests) > 4096 {
		return ErrStorage
	}
	for _, digest := range progress.ChunkDigests {
		if !validDigest(digest) {
			return ErrStorage
		}
	}
	if !slices.Contains([]string{"pending", "downloading", "publishing", "published", "collecting", "finalizing", "finalized"}, progress.Phase) {
		return ErrStorage
	}
	return nil
}

func validTransferTiming(progress *transferProgress) bool {
	return progress.StartedAt >= 1 && progress.DeadlineAt > progress.StartedAt && progress.DeadlineAt-progress.StartedAt <= int64(protocol.MaximumTimeoutSeconds)*1000
}

func validSourceProgress(progress *transferProgress) bool {
	if progress.Source == nil {
		return true
	}
	return progress.Source.Size >= 0 && progress.Source.Size <= protocol.MaximumFileBytes && validDigest(progress.Source.Digest) && progress.SourceVersion != "" && len(progress.SourceVersion) <= 256
}

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && hex.EncodeToString(data) == value
}

func chunkCount(size int64) int {
	return int((size + protocol.FileChunkBytes - 1) / protocol.FileChunkBytes)
}

func chunkLength(size int64, index int) (int, error) {
	if size < 0 || size > protocol.MaximumFileBytes || index < 0 || index >= chunkCount(size) {
		return 0, ErrPolicy
	}
	return int(min(int64(protocol.FileChunkBytes), size-int64(index)*protocol.FileChunkBytes)), nil
}

type contextFileReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextFileReader) Read(value []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(value)
}

func digestFile(ctx context.Context, file *os.File, maximum int64) (protocol.SourceInfo, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return protocol.SourceInfo{}, ErrStorage
	}
	digest := sha256.New()
	size, err := io.CopyBuffer(digest, io.LimitReader(contextFileReader{ctx, file}, maximum+1), make([]byte, 128<<10))
	if err != nil {
		return protocol.SourceInfo{}, err
	}
	if size > maximum {
		return protocol.SourceInfo{}, ErrPolicy
	}
	return protocol.SourceInfo{Size: size, Digest: hex.EncodeToString(digest.Sum(nil))}, nil
}
