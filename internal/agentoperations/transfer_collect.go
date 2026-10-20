package agentoperations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func decodeFileReceipt(reader io.Reader) error {
	content, err := io.ReadAll(io.LimitReader(reader, maximumJournalBytes+1))
	if err != nil || len(content) > maximumJournalBytes {
		return ErrTransport
	}
	var receipt protocol.AgentResponse[protocol.FileTransferReceipt]
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil || receipt.Code != 200 || !receipt.Data.Acknowledged {
		return ErrTransport
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrTransport
	}
	return nil
}

type transferJob struct {
	record  *journal
	root    FileRoot
	gateway FileGateway
	save    func() error
}

func (job transferJob) collect(ctx context.Context) (resultErr error) {
	root, err := openTransferRoot(job.root.Path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	source, err := root.openSource(job.record.Lease.Payload.File.RelativePath)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	if err := job.captureSource(ctx, source); err != nil {
		return err
	}
	receipt, err := job.gateway.DeclareSource(ctx, job.record.Lease, *job.record.Transfer.Source)
	if err != nil {
		return err
	}
	if err := job.uploadMissing(ctx, source, receipt.MissingChunks); err != nil {
		return err
	}
	if err := job.verifySource(ctx, source); err != nil {
		return err
	}
	job.record.Transfer.Phase = "finalizing"
	if err := job.save(); err != nil {
		return err
	}
	return job.finalizeCollect(ctx)
}

func (job transferJob) finalizeCollect(ctx context.Context) error {
	if err := job.gateway.FinalizeFile(ctx, job.record.Lease); err != nil {
		return err
	}
	job.record.Transfer.Phase = "finalized"
	return job.save()
}

func (job transferJob) captureSource(ctx context.Context, source *os.File) error {
	before, err := transferFileVersion(source)
	if err != nil {
		return err
	}
	info, err := digestFile(ctx, source, min(job.root.MaximumBytes, protocol.MaximumFileBytes))
	if err != nil {
		return err
	}
	after, err := transferFileVersion(source)
	if err != nil || before != after {
		return ErrSourceChanged
	}
	progress := job.record.Transfer
	if progress.Source != nil {
		if *progress.Source != info || progress.SourceVersion != after {
			return ErrSourceChanged
		}
		return nil
	}
	progress.Source, progress.SourceVersion = &info, after
	progress.Phase = "collecting"
	return job.save()
}

func (job transferJob) verifySource(ctx context.Context, source *os.File) error {
	info, err := digestFile(ctx, source, job.root.MaximumBytes)
	if err != nil {
		return err
	}
	version, err := transferFileVersion(source)
	if err != nil {
		return err
	}
	if info != *job.record.Transfer.Source || version != job.record.Transfer.SourceVersion {
		return ErrSourceChanged
	}
	return nil
}

func (job transferJob) uploadMissing(ctx context.Context, source *os.File, missing []int) error {
	total := job.record.Transfer.Source.Size
	if len(missing) > chunkCount(total) {
		return ErrFileIntegrity
	}
	seen := make(map[int]bool, len(missing))
	for _, index := range missing {
		if seen[index] {
			return ErrFileIntegrity
		}
		seen[index] = true
		if err := job.uploadSourceChunk(ctx, source, FileChunk{Index: index, Total: total}); err != nil {
			return err
		}
	}
	return nil
}

func (job transferJob) uploadSourceChunk(ctx context.Context, source *os.File, chunk FileChunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	length, err := chunkLength(chunk.Total, chunk.Index)
	if err != nil {
		return err
	}
	chunk.Data = make([]byte, length)
	defer clear(chunk.Data)
	count, err := source.ReadAt(chunk.Data, int64(chunk.Index)*protocol.FileChunkBytes)
	if err != nil || count != length {
		return ErrSourceChanged
	}
	return job.gateway.UploadChunk(ctx, job.record.Lease, chunk)
}
