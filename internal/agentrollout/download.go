package agentrollout

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

var ErrBusy = errors.New("agent rollout transfer capacity reached")

type DownloadRequest struct {
	Identity                        model.AgentIdentity
	AssignmentID, ArtifactID, Nonce string
}

type Download struct {
	service   *Service
	ctx       context.Context
	request   DownloadRequest
	file      *os.File
	info      os.FileInfo
	Artifact  protocol.Artifact
	ExpiresAt time.Time
	once      sync.Once
}

func (s *Service) OpenDownload(ctx context.Context, input DownloadRequest) (*Download, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	select {
	case s.transfers <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	value := &Download{service: s, ctx: ctx, request: input}
	artifact, expires, err := s.downloadAuthorization(ctx, input)
	if err != nil {
		_ = value.Close()
		return nil, err
	}
	value.Artifact, value.ExpiresAt = artifact, expires
	value.file, err = openArtifactFile(filepath.Join(s.directory, artifact.SHA256+artifactFileSuffix))
	if err == nil {
		err = value.verify()
	}
	if err == nil {
		_, _, err = s.downloadAuthorization(ctx, input)
	}
	if err != nil {
		_ = value.Close()
		return nil, err
	}
	return value, nil
}

func (d *Download) verify() error {
	info, err := d.file.Stat()
	if err != nil || !ownedReleaseFile(info) || info.Mode().Perm()&0o077 != 0 || info.Size() != d.Artifact.Size {
		return ErrUnavailable
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(contextReader{d.ctx, d.file}, d.Artifact.Size+1))
	if err != nil || written != d.Artifact.Size || hex.EncodeToString(hash.Sum(nil)) != d.Artifact.SHA256 {
		return ErrUnavailable
	}
	if _, err := d.file.Seek(0, io.SeekStart); err != nil {
		return ErrUnavailable
	}
	d.info = info
	return nil
}

func (d *Download) Read(p []byte) (int, error) {
	if !d.service.now().Before(d.ExpiresAt) {
		return 0, ErrForbidden
	}
	if _, _, err := d.service.downloadAuthorization(d.ctx, d.request); err != nil {
		return 0, err
	}
	info, err := d.file.Stat()
	if err != nil || info.Size() != d.info.Size() || !info.ModTime().Equal(d.info.ModTime()) {
		return 0, ErrUnavailable
	}
	return d.file.Read(p)
}

func (d *Download) Seek(offset int64, whence int) (int64, error) { return d.file.Seek(offset, whence) }

func (d *Download) Close() error {
	var err error
	d.once.Do(func() {
		if d.file != nil {
			err = d.file.Close()
		}
		<-d.service.transfers
	})
	return err
}

func (s *Service) downloadAuthorization(ctx context.Context, input DownloadRequest) (protocol.Artifact, time.Time, error) {
	var artifact protocol.Artifact
	var expiry time.Time
	if !validDownloadRequest(input) {
		return artifact, expiry, ErrInvalid
	}
	err := s.agentMutation(ctx, input.Identity, func(tx *transaction, node nodeState) error {
		target, err := tx.downloadTarget(input)
		if err != nil {
			return err
		}
		expiry = target.expires

		rollout, err := tx.rollout(target.rolloutID)
		if err != nil {
			return err
		}
		if !downloadWindow(rollout, tx.at) {
			return ErrConflict
		}
		release, _, err := s.releaseInTransaction(tx, rollout.ReleaseID)
		if err != nil {
			return err
		}
		artifact, err = findArtifact(release.Manifest, input.ArtifactID)
		if err != nil {
			return err
		}
		if artifact.OS != node.os || !sameArchitecture(artifact.Arch, node.arch) {
			return ErrForbidden
		}
		return tx.readyArtifact(release.ID, artifact)
	})
	return artifact, expiry, err
}

func (tx *transaction) readyArtifact(releaseID string, artifact protocol.Artifact) error {
	var state, digest string
	var size int64
	err := tx.db.QueryRowContext(tx.ctx, "SELECT state,digest,size FROM agent_release_artifacts WHERE release_id=? AND artifact_id=?", releaseID, artifact.ID).Scan(&state, &digest, &size)
	if err != nil || state != "ready" || digest != artifact.SHA256 || size != artifact.Size {
		return ErrUnavailable
	}
	return nil
}

type downloadTarget struct {
	rolloutID, state, artifactID string
	nonce                        []byte
	expires                      time.Time
}

func (tx *transaction) downloadTarget(input DownloadRequest) (downloadTarget, error) {
	var value downloadTarget
	err := tx.db.QueryRowContext(tx.ctx, "SELECT rollout_id,state,artifact_id,nonce_hash,lease_expires_at FROM agent_rollout_targets WHERE node_id=? AND current_attempt_id=?", input.Identity.NodeID, input.AssignmentID).Scan(&value.rolloutID, &value.state, &value.artifactID, &value.nonce, &value.expires)
	if err != nil {
		return value, ErrNotFound
	}
	hash := sha256.Sum256([]byte(input.Nonce))
	if value.artifactID != input.ArtifactID || value.state != "offered" || !tx.at.Before(value.expires) || subtle.ConstantTimeCompare(value.nonce, hash[:]) != 1 {
		return value, ErrForbidden
	}
	return value, nil
}

func validDownloadRequest(input DownloadRequest) bool {
	return len(input.Nonce) == 2*assignmentNonceBytes && len(input.AssignmentID) <= maximumIdentifierBytes && len(input.ArtifactID) <= maximumIdentifierBytes
}

func downloadWindow(rollout Rollout, at time.Time) bool {
	return rollout.State == "active" && !at.Before(rollout.Policy.WindowStart) && at.Before(rollout.Policy.WindowEnd)
}
