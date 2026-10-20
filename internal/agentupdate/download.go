package agentupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func (u *Updater) download(ctx context.Context, inst *installation, value *journal) (resultErr error) {
	if u.config.Fetcher == nil {
		return ErrConfiguration
	}
	file, err := inst.state.open(downloadFile, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < value.Downloaded {
		return ErrJournal
	}
	if err = file.Truncate(value.Downloaded); err != nil {
		return err
	}
	hash := sha256.New()
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = io.CopyN(hash, &contextReader{ctx: ctx, reader: file}, value.Downloaded); err != nil {
		return err
	}
	if err = u.transferChunks(ctx, downloadProgress{inst: inst, value: value, file: file, hash: hash}); err != nil {
		return err
	}

	if hex.EncodeToString(hash.Sum(nil)) != value.Assignment.Artifact.SHA256 {
		return ErrIntegrity
	}
	return imageVerifier(u.config.Arch).verifyImage(ctx, file, imageFor(value))
}

func (u *Updater) downloadChunk(ctx context.Context, writer io.Writer, input FetchRequest) (resultErr error) {
	response, err := u.config.Fetcher.Fetch(ctx, input)
	if err != nil {
		return err
	}
	if response.Body == nil {
		return ErrDownload
	}
	defer func() { resultErr = errors.Join(resultErr, response.Body.Close()) }()
	if response.Offset != input.Offset || response.Length != input.Length || response.Total != input.Assignment.Artifact.Size {
		return ErrDownload
	}
	count, err := io.CopyN(writer, &contextReader{ctx: ctx, reader: response.Body}, input.Length)
	if err != nil || count != input.Length {
		return ErrDownload
	}
	var extra [1]byte
	countExtra, err := response.Body.Read(extra[:])
	if countExtra != 0 || !errors.Is(err, io.EOF) {
		return ErrDownload
	}
	return nil
}

type downloadProgress struct {
	inst  *installation
	value *journal
	file  *os.File
	hash  hash.Hash
}

func (u *Updater) transferChunks(ctx context.Context, progress downloadProgress) error {
	inst, value, file, hash := progress.inst, progress.value, progress.file, progress.hash
	manifest, err := protocol.DecodeManifest(value.Assignment.Manifest.Payload)
	if err != nil {
		return err
	}
	for value.Downloaded < value.Assignment.Artifact.Size {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !u.config.Now().Before(value.Assignment.ExpiresAt) || !u.config.Now().Before(manifest.ExpiresAt) {
			return ErrAssignment
		}
		length := min(ChunkBytes, value.Assignment.Artifact.Size-value.Downloaded)
		if err = u.downloadChunk(ctx, io.MultiWriter(file, hash), FetchRequest{Assignment: value.Assignment, Offset: value.Downloaded, Length: length}); err != nil {
			return err
		}
		if err = file.Sync(); err != nil {
			return err
		}
		value.Downloaded += length
		if err = saveJournal(inst, value); err != nil {
			return err
		}
		u.observe("downloading", "", value.Downloaded)
		if err = u.checkpoint("download_chunk"); err != nil {
			return err
		}
	}
	return nil
}
