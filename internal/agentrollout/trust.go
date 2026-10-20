package agentrollout

import (
	"bytes"
	"context"
	"database/sql"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/releasesignature"
)

func (s *Service) initializeTrust(ctx context.Context) error {
	db, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = db.Rollback() }()
	tx := &transaction{ctx: ctx, db: db, at: s.now(), username: "system"}
	for _, key := range s.keys {
		if err := tx.registerTrustedKey(key); err != nil {
			return err
		}
	}
	if err := s.persistRuntimePolicy(tx); err != nil {
		return err
	}
	if err := db.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) verifySigned(ctx context.Context, signed protocol.SignedManifest) (protocol.Manifest, error) {
	manifest, err := decodeManifest(signed.Payload)
	if err != nil {
		return manifest, err
	}
	if !s.compatibleManifest(manifest) {
		return manifest, ErrManifest
	}
	if err := validateManifest(manifest, s.now()); err != nil {
		return manifest, err
	}
	if err := s.verifyPinnedKey(signed, manifest); err != nil {
		return manifest, err
	}
	if err := s.verifySigstore(ctx, signed, manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func (s *Service) verifyPinnedKey(signed protocol.SignedManifest, manifest protocol.Manifest) error {
	key, exists := s.keys[manifest.SigningKeyID]
	if !exists {
		return ErrSignature
	}
	_, err := protocol.VerifyManifestSignature(signed, protocol.SigningTrust{KeyID: key.ID, PublicKey: key.PublicKey, NotBefore: key.NotBefore, ExpiresAt: key.ExpiresAt}, s.now())
	return err
}

func (s *Service) verifySigstore(ctx context.Context, signed protocol.SignedManifest, manifest protocol.Manifest) error {
	if s.pinnedOnly {
		// 固定公钥模式只认 Ed25519 清单签名：既没有信任锚可以校验 Sigstore 证据，
		// 也不把这种证据记成「已用 Sigstore 验证」，因此携带 bundle 的发布直接拒绝。
		if len(signed.SigstoreBundle) != 0 {
			return ErrSignature
		}
		return nil
	}
	if s.sigstore == nil || len(signed.SigstoreBundle) == 0 || len(signed.SigstoreBundle) > releasesignature.MaximumBundleBytes {
		return ErrSignature
	}
	tag, err := ReleaseTag(manifest.Version)
	if err != nil {
		return err
	}
	if err := s.sigstore.Verify(ctx, releasesignature.Evidence{Payload: signed.Payload, Bundle: signed.SigstoreBundle, ReleaseTag: tag}); err != nil {
		return ErrSignature
	}
	return nil
}

func (tx *transaction) keyAvailable(id string) error {
	var revoked sql.NullTime
	err := tx.db.QueryRowContext(tx.ctx, "SELECT revoked_at FROM agent_release_trust WHERE key_id=? AND not_before<=? AND expires_at>?", id, tx.at, tx.at).Scan(&revoked)
	if err != nil || revoked.Valid {
		return ErrSignature
	}
	return nil
}

func (s *Service) RevokeKey(ctx context.Context, actor Actor, id string) error {
	return s.mutate(ctx, actor, func(tx *transaction) error {
		result, err := tx.db.ExecContext(ctx, "UPDATE agent_release_trust SET revoked_at=?,revision=revision+1 WHERE key_id=?", tx.at, id)
		if err := requireOne(result, err); err != nil {
			return err
		}
		if err := tx.exec(`UPDATE agent_releases SET revoked_at=? WHERE signing_key_id=?`, tx.at, id); err != nil {
			return err
		}
		if err := tx.exec(`UPDATE agent_rollouts SET state='paused',reason='signing_key_revoked',updated_at=? WHERE state='active' AND release_id IN(SELECT id FROM agent_releases WHERE signing_key_id=?)`, tx.at, id); err != nil {
			return err
		}
		return tx.audit(audit{Action: "release.key.revoked", Kind: "release_key", ID: id})
	})
}

func (tx *transaction) keyRevision(id string) (int64, error) {
	if err := tx.keyAvailable(id); err != nil {
		return 0, err
	}
	var revision int64
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT revision FROM agent_release_trust WHERE key_id=?", id).Scan(&revision); err != nil {
		return 0, ErrUnavailable
	}
	return revision, nil
}

func (s *Service) trustedRevision(tx *transaction, id string) (int64, error) {
	key, exists := s.keys[id]
	if !exists {
		return 0, ErrSignature
	}
	revision, err := tx.keyRevision(id)
	if err != nil {
		return 0, err
	}
	var public []byte
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT public_key FROM agent_release_trust WHERE key_id=?", id).Scan(&public); err != nil {
		return 0, ErrUnavailable
	}
	if !bytes.Equal(public, key.PublicKey) {
		return 0, ErrSignature
	}
	return revision, nil
}

func (s *Service) compatibleManifest(manifest protocol.Manifest) bool {
	return manifest.MinimumServerSchema <= s.schema && manifest.MinimumProtocol <= s.protocol && manifest.MaximumProtocol >= s.protocol
}

func (s *Service) requireTrustRevision(tx *transaction, id string, expected int64) error {
	current, err := s.trustedRevision(tx, id)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrSignature
	}
	return nil
}

func (tx *transaction) registerTrustedKey(key TrustedKey) error {
	result, err := tx.db.ExecContext(tx.ctx, `INSERT INTO agent_release_trust(key_id,public_key,not_before,expires_at,created_at) VALUES(?,?,?,?,?) ON CONFLICT(key_id) DO NOTHING`, key.ID, []byte(key.PublicKey), key.NotBefore, key.ExpiresAt, tx.at)
	if err != nil {
		return ErrUnavailable
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return ErrUnavailable
	}
	var stored []byte
	if err := tx.db.QueryRowContext(tx.ctx, "SELECT public_key FROM agent_release_trust WHERE key_id=?", key.ID).Scan(&stored); err != nil {
		return ErrUnavailable
	}
	if !bytes.Equal(stored, key.PublicKey) {
		return ErrSignature
	}
	if inserted == 1 {
		if err := tx.audit(audit{Action: "release.trust.loaded", Kind: "release_key", ID: key.ID}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) persistRuntimePolicy(tx *transaction) error {
	if s.signaturePolicy == nil {
		return ErrSignature
	}
	return tx.persistSignaturePolicy(*s.signaturePolicy)
}
