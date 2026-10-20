package agentrollout

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

type ArtifactUpload struct {
	ReleaseID, ArtifactID string
	Body                  io.Reader
}

func (s *Service) PutArtifact(ctx context.Context, actor Actor, input ArtifactUpload) error {
	var artifact protocol.Artifact
	err := s.mutate(ctx, actor, func(tx *transaction) error {
		release, _, err := s.releaseInTransaction(tx, input.ReleaseID)
		if err != nil {
			return err
		}
		artifact, err = findArtifact(release.Manifest, input.ArtifactID)
		return err
	})
	if err != nil {
		return err
	}
	if input.Body == nil {
		return ErrInvalid
	}
	path, err := s.stageArtifact(ctx, artifact, input.Body)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(path) }()
	if err := s.publishArtifact(path, artifact); err != nil {
		return err
	}
	return s.mutate(ctx, actor, func(tx *transaction) error {
		if _, _, err := s.releaseInTransaction(tx, input.ReleaseID); err != nil {
			return err
		}
		result, err := tx.db.ExecContext(tx.ctx, "UPDATE agent_release_artifacts SET state='ready',updated_at=? WHERE release_id=? AND artifact_id=? AND digest=? AND size=?", tx.at, input.ReleaseID, input.ArtifactID, artifact.SHA256, artifact.Size)
		if err := requireOne(result, err); err != nil {
			return err
		}
		return tx.audit(audit{Action: "release.artifact.import", Kind: "release", ID: input.ReleaseID})
	})
}

func findArtifact(manifest protocol.Manifest, id string) (protocol.Artifact, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.ID == id {
			return artifact, nil
		}
	}
	return protocol.Artifact{}, ErrNotFound
}

func (s *Service) stageArtifact(ctx context.Context, artifact protocol.Artifact, body io.Reader) (string, error) {
	file, err := os.CreateTemp(s.directory, ".artifact-")
	if err != nil {
		return "", ErrUnavailable
	}
	path := file.Name()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx, body}, artifact.Size+1))
	err = errors.Join(copyErr, file.Sync(), file.Close())
	if err != nil || written != artifact.Size || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		_ = os.Remove(path)
		return "", ErrManifest
	}
	return path, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *Service) publishArtifact(staged string, artifact protocol.Artifact) error {
	destination := filepath.Join(s.directory, artifact.SHA256+artifactFileSuffix)
	if err := os.Link(staged, destination); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return ErrUnavailable
		}
		if err := s.checkArtifact(artifact); err == nil {
			return nil
		}
		info, err := os.Lstat(destination)
		if err != nil || !info.Mode().IsRegular() {
			return ErrUnavailable
		}
		if err := os.Rename(staged, destination); err != nil {
			return ErrUnavailable
		}
	}
	directory, err := os.Open(s.directory)
	if err != nil {
		return ErrUnavailable
	}
	if err := errors.Join(directory.Sync(), directory.Close()); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) checkArtifact(artifact protocol.Artifact) error {
	path := filepath.Join(s.directory, artifact.SHA256+artifactFileSuffix)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !ownedReleaseFile(info) || info.Mode().Perm()&0o077 != 0 || info.Size() != artifact.Size {
		return ErrUnavailable
	}
	file, err := openArtifactFile(path)
	if err != nil {
		return ErrUnavailable
	}
	hash := sha256.New()
	written, copyErr := io.Copy(hash, io.LimitReader(file, artifact.Size+1))
	err = errors.Join(copyErr, file.Close())
	if err != nil || written != artifact.Size || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		return ErrUnavailable
	}
	return nil
}
