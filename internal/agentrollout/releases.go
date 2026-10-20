package agentrollout

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

type Release struct {
	ID       string            `json:"id"`
	Manifest protocol.Manifest `json:"manifest"`
	Verified bool              `json:"verified"`
	Revoked  bool              `json:"revoked"`
}

func (s *Service) Import(ctx context.Context, actor Actor, signed protocol.SignedManifest) (Release, error) {
	var output Release
	if err := s.requireEnabled(); err != nil {
		return output, err
	}
	preliminary, err := decodeManifest(signed.Payload)
	if err != nil {
		return output, err
	}
	revision, policyRevision, err := s.importTrustSnapshot(ctx, actor, preliminary.SigningKeyID)
	if err != nil {
		return output, err
	}
	manifest, err := s.verifySigned(ctx, signed)
	if err != nil {
		return output, err
	}
	id := releaseDigest(signed.Payload)
	err = s.mutate(ctx, actor, func(tx *transaction) error {
		if err := s.requireTrustRevision(tx, manifest.SigningKeyID, revision); err != nil {
			return err
		}
		if err := tx.persistRelease(releaseWrite{id: id, manifest: manifest, signed: signed, revision: revision, policyRevision: policyRevision}); err != nil {
			return err
		}
		output = Release{ID: id, Manifest: manifest, Verified: true}
		if err := tx.audit(audit{Action: "release.import", Kind: "release", ID: id, Count: len(manifest.Artifacts)}); err != nil {
			return err
		}
		tx.at = s.now()
		if err := s.requireTrustRevision(tx, manifest.SigningKeyID, revision); err != nil {
			return err
		}
		return s.requireSignaturePolicyRevision(tx, policyRevision)
	})
	if err != nil {
		return Release{}, err
	}
	return output, nil
}

type releaseWrite struct {
	id             string
	manifest       protocol.Manifest
	signed         protocol.SignedManifest
	revision       int64
	policyRevision string
}

func (tx *transaction) persistRelease(input releaseWrite) error {
	id, manifest, signed := input.id, input.manifest, input.signed
	// 固定公钥模式没有 Sigstore 证据，但这一列是 NOT NULL：把缺省值规范成空 blob，
	// 让「没有证据」与「读取失败」在库里可区分。
	bundle := signed.SigstoreBundle
	if bundle == nil {
		bundle = []byte{}
	}
	var previous string
	err := tx.db.QueryRowContext(tx.ctx, "SELECT id FROM agent_releases WHERE version=? AND channel=?", manifest.Version, manifest.Channel).Scan(&previous)
	if err == nil && previous != id {
		return ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrUnavailable
	}
	if err := tx.exec(`INSERT INTO agent_releases(id,version,channel,signing_key_id,payload,signature,sigstore_bundle,expires_at,trust_revision,signature_policy_revision,verified,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,1,?,?) ON CONFLICT(id) DO UPDATE SET verified=1,trust_revision=excluded.trust_revision,signature_policy_revision=excluded.signature_policy_revision,signature=excluded.signature,sigstore_bundle=excluded.sigstore_bundle WHERE agent_releases.revoked_at IS NULL`, id, manifest.Version, manifest.Channel, manifest.SigningKeyID, signed.Payload, signed.Signature, bundle, manifest.ExpiresAt, input.revision, input.policyRevision, tx.actor.UserID, tx.at); err != nil {
		return err
	}
	var revoked sql.NullTime
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT revoked_at FROM agent_releases WHERE id=?", id).Scan(&revoked); err != nil {
		return ErrUnavailable
	}
	if revoked.Valid {
		return ErrConflict
	}
	return tx.persistArtifactMetadata(id, manifest.Artifacts)
}

type storedRelease struct {
	release                 Release
	signed                  protocol.SignedManifest
	revoked                 sql.NullTime
	version, channel, keyID string
	trustRevision           int64
	policyRevision          string
}

func (tx *transaction) loadRelease(id string) (storedRelease, error) {
	var value storedRelease
	err := tx.db.QueryRowContext(tx.ctx, "SELECT payload,signature,sigstore_bundle,verified,revoked_at,version,channel,signing_key_id,trust_revision,signature_policy_revision FROM agent_releases WHERE id=?", id).Scan(&value.signed.Payload, &value.signed.Signature, &value.signed.SigstoreBundle, &value.release.Verified, &value.revoked, &value.version, &value.channel, &value.keyID, &value.trustRevision, &value.policyRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, ErrUnavailable
	}
	return value, nil
}

func (s *Service) releaseInTransaction(tx *transaction, id string) (Release, protocol.SignedManifest, error) {
	value, err := tx.loadRelease(id)
	if err != nil {
		return value.release, value.signed, err
	}
	manifest, err := decodeManifest(value.signed.Payload)
	if err != nil {
		return value.release, value.signed, err
	}
	if !value.matches(manifest, id) {
		return value.release, value.signed, ErrManifest
	}
	if err := validateManifest(manifest, tx.at); err != nil {
		return value.release, value.signed, err
	}
	if err := s.verifyCachedSignature(value.signed, manifest, tx.at); err != nil {
		return value.release, value.signed, err
	}
	if err := s.availableRelease(tx, value, manifest); err != nil {
		return value.release, value.signed, err
	}
	value.release.ID, value.release.Manifest = id, manifest
	return value.release, value.signed, nil
}

func (value storedRelease) matches(manifest protocol.Manifest, id string) bool {
	return releaseDigest(value.signed.Payload) == id && manifest.Version == value.version && manifest.Channel == value.channel && manifest.SigningKeyID == value.keyID
}

func (s *Service) verifyCachedSignature(signed protocol.SignedManifest, manifest protocol.Manifest, at time.Time) error {
	key, exists := s.keys[manifest.SigningKeyID]
	if !exists {
		return ErrSignature
	}
	_, err := protocol.VerifyManifestSignature(signed, protocol.SigningTrust{KeyID: key.ID, PublicKey: key.PublicKey, NotBefore: key.NotBefore, ExpiresAt: key.ExpiresAt}, at)
	return err
}

func (s *Service) RevokeRelease(ctx context.Context, actor Actor, id string) error {
	return s.mutate(ctx, actor, func(tx *transaction) error {
		result, err := tx.db.ExecContext(ctx, "UPDATE agent_releases SET revoked_at=? WHERE id=?", tx.at, id)
		if err := requireOne(result, err); err != nil {
			return err
		}
		if err := tx.exec("UPDATE agent_rollouts SET state='paused',reason='release_revoked',updated_at=? WHERE release_id=? AND state='active'", tx.at, id); err != nil {
			return err
		}
		return tx.audit(audit{Action: "release.revoke", Kind: "release", ID: id})
	})
}

func (tx *transaction) persistArtifactMetadata(id string, artifacts []protocol.Artifact) error {
	for _, artifact := range artifacts {
		if err := tx.exec(`INSERT INTO agent_release_artifacts(release_id,artifact_id,digest,size,state,updated_at) VALUES(?,?,?,?,'missing',?) ON CONFLICT(release_id,artifact_id) DO NOTHING`, id, artifact.ID, artifact.SHA256, artifact.Size, tx.at); err != nil {
			return err
		}
	}
	var count int
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT COUNT(*) FROM agent_release_artifacts WHERE release_id=?", id).Scan(&count); err != nil || count != len(artifacts) {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) availableRelease(tx *transaction, value storedRelease, manifest protocol.Manifest) error {
	if err := s.requireSignaturePolicyRevision(tx, value.policyRevision); err != nil {
		return err
	}
	revision, err := s.trustedRevision(tx, manifest.SigningKeyID)
	if err != nil {
		return err
	}
	if value.revoked.Valid || !value.release.Verified || value.trustRevision != revision {
		return ErrConflict
	}
	if !s.compatibleManifest(manifest) {
		return ErrManifest
	}
	return nil
}

func (s *Service) importTrustSnapshot(ctx context.Context, actor Actor, keyID string) (int64, string, error) {
	var revision int64
	var policy string
	err := s.mutate(ctx, actor, func(tx *transaction) error {
		var err error
		revision, err = s.trustedRevision(tx, keyID)
		if err != nil {
			return err
		}
		policy, err = s.signaturePolicyRevision(tx)
		return err
	})
	return revision, policy, err
}
