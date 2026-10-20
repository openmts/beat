package agentoperations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

func (job transferJob) deploy(ctx context.Context) (resultErr error) {
	root, err := openTransferRoot(job.root.Path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	destination, err := root.destination(job.record.Lease.Payload.File.RelativePath)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, destination.Close()) }()
	if job.record.Transfer.Phase == "published" {
		return nil
	}
	if job.record.Transfer.Phase == "publishing" {
		return job.reconcilePublication(ctx, destination)
	}
	return job.deployPrepared(ctx, destination)
}

func (job transferJob) deployPrepared(ctx context.Context, destination *transferDestination) (resultErr error) {
	if err := checkDestination(destination, job.record.Lease.Payload.File.Overwrite); err != nil {
		return err
	}
	file, err := job.prepareTemporary(destination)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := job.verifyPrefix(ctx, file); err != nil {
		return err
	}
	if err := job.downloadMissing(ctx, file); err != nil {
		return err
	}
	if err := job.preparePublication(ctx, file); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := destination.publish(job.record.Transfer.Temporary, job.record.Lease.Payload.File.Overwrite); err != nil {
		return err
	}
	job.record.Transfer.Phase = "published"
	return job.save()
}

func (job transferJob) prepareTemporary(destination *transferDestination) (*os.File, error) {
	progress := job.record.Transfer
	if progress.Temporary == "" {
		name, err := temporaryName()
		if err != nil {
			return nil, err
		}
		progress.Temporary = name
		progress.Phase = "downloading"
		if err := job.save(); err != nil {
			return nil, err
		}
	}
	file, err := destination.temporary(progress.Temporary, false)
	if nativeNotExist(err) && len(progress.ChunkDigests) == 0 {
		return destination.temporary(progress.Temporary, true)
	}
	return file, err
}

func (job transferJob) verifyPrefix(ctx context.Context, file *os.File) error {
	size := job.record.Lease.Payload.File.Size
	if len(job.record.Transfer.ChunkDigests) > chunkCount(size) {
		return ErrFileIntegrity
	}
	var expected int64
	for index, want := range job.record.Transfer.ChunkDigests {
		if err := ctx.Err(); err != nil {
			return err
		}
		length, err := chunkLength(size, index)
		if err != nil {
			return err
		}
		content := make([]byte, length)
		count, readErr := file.ReadAt(content, expected)
		digest := sha256.Sum256(content)
		clear(content)
		if readErr != nil || count != length || hex.EncodeToString(digest[:]) != want {
			return ErrFileIntegrity
		}
		expected += int64(length)
	}
	if err := file.Truncate(expected); err != nil {
		return ErrStorage
	}
	_, err := file.Seek(expected, io.SeekStart)
	return err
}

func (job transferJob) downloadMissing(ctx context.Context, file *os.File) error {
	for len(job.record.Transfer.ChunkDigests) < chunkCount(job.record.Lease.Payload.File.Size) {
		index := len(job.record.Transfer.ChunkDigests)
		if err := job.downloadChunk(ctx, file, index); err != nil {
			return err
		}
	}
	return nil
}

func (job transferJob) downloadChunk(ctx context.Context, file *os.File, index int) error {
	content, err := job.gateway.DownloadChunk(ctx, job.record.Lease, index)
	if err != nil {
		return err
	}
	defer clear(content)
	expected, err := chunkLength(job.record.Lease.Payload.File.Size, index)
	if err != nil || len(content) != expected {
		return ErrFileIntegrity
	}
	digest := sha256.Sum256(content)
	count, err := file.Write(content)
	if err != nil || count != len(content) {
		return ErrStorage
	}
	if err := file.Sync(); err != nil {
		return ErrStorage
	}
	job.record.Transfer.ChunkDigests = append(job.record.Transfer.ChunkDigests, hex.EncodeToString(digest[:]))
	return job.save()
}

func (job transferJob) preparePublication(ctx context.Context, file *os.File) error {
	spec := job.record.Lease.Payload.File
	source, err := digestFile(ctx, file, spec.Size)
	if err != nil {
		return err
	}
	if source.Size != spec.Size || source.Digest != spec.Digest {
		return ErrFileIntegrity
	}
	if err := setTransferMode(file, spec.Mode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return ErrStorage
	}
	identity, err := transferFileIdentity(file)
	if err != nil {
		return err
	}
	job.record.Transfer.TemporaryIdentity = identity
	job.record.Transfer.Phase = "publishing"
	return job.save()
}

func (job transferJob) reconcilePublication(ctx context.Context, destination *transferDestination) error {
	temporary, err := destination.temporary(job.record.Transfer.Temporary, false)
	if err == nil {
		verifyErr := job.verifyPublishedFile(ctx, temporary)
		closeErr := temporary.Close()
		if err := errors.Join(verifyErr, closeErr); err != nil {
			return err
		}
		if err := destination.publish(job.record.Transfer.Temporary, job.record.Lease.Payload.File.Overwrite); err != nil {
			return err
		}
	} else if nativeNotExist(err) {
		if err := job.verifyDestination(ctx, destination); err != nil {
			return err
		}
	} else {
		return err
	}
	job.record.Transfer.Phase = "published"
	return job.save()
}

func (job transferJob) verifyDestination(ctx context.Context, destination *transferDestination) (resultErr error) {
	current, err := destination.current()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, current.Close()) }()
	return job.verifyPublishedFile(ctx, current)
}

func (job transferJob) verifyPublishedFile(ctx context.Context, file *os.File) error {
	identity, err := transferFileIdentity(file)
	if err != nil || identity != job.record.Transfer.TemporaryIdentity {
		return ErrFileConflict
	}
	spec := job.record.Lease.Payload.File
	if !transferModeMatches(file, spec.Mode) {
		return ErrFileIntegrity
	}
	digest, err := digestFile(ctx, file, spec.Size)
	if err != nil {
		return err
	}
	if digest.Size != spec.Size || digest.Digest != spec.Digest {
		return ErrFileIntegrity
	}
	return nil
}

func checkDestination(destination *transferDestination, overwrite bool) error {
	current, err := destination.current()
	if nativeNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := current.Close(); err != nil {
		return err
	}
	if !overwrite {
		return ErrFileConflict
	}
	return nil
}
