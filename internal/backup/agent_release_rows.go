package backup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

type archivedReleaseRow struct {
	id, version, channel, keyID  string
	payload                      []byte
	payloadBytes, signatureBytes int64
	expires                      time.Time
	trustExists, verified        int
}

const archivedReleaseQuery = `SELECT CASE WHEN length(r.id)=64 THEN r.id END,
 CASE WHEN length(r.version)<=? THEN r.version END,
 CASE WHEN length(r.channel)<=7 THEN r.channel END,CASE WHEN length(r.signing_key_id)<=128 THEN r.signing_key_id END,
 CASE WHEN length(CAST(r.payload AS BLOB))<=? THEN r.payload END,length(CAST(r.payload AS BLOB)),length(CAST(r.signature AS BLOB)),r.expires_at,
 EXISTS(SELECT 1 FROM agent_release_trust t WHERE t.key_id=r.signing_key_id),r.verified
 FROM agent_releases r ORDER BY r.id LIMIT ?`

func archivedReleaseArtifacts(ctx context.Context, database *sql.DB) (artifacts map[string]model.BackupAgentReleaseArtifact, resultErr error) {
	rows, err := database.QueryContext(ctx, archivedReleaseQuery, protocol.MaximumIdentifierBytes+1, protocol.MaximumManifestBytes, maximumArchivedAgentReleases+1)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	artifacts = map[string]model.BackupAgentReleaseArtifact{}
	releases := 0
	var proofBytes int64
	for rows.Next() {
		if releases >= maximumArchivedAgentReleases {
			return nil, errAgentReleaseArchive
		}
		releases++
		var row archivedReleaseRow
		if err = row.scan(rows); err != nil {
			return nil, err
		}
		proofBytes += row.payloadBytes
		if row.payloadBytes < 1 || row.payloadBytes > protocol.MaximumManifestBytes || proofBytes > maximumArchivedAgentProofBytes {
			return nil, errAgentReleaseArchive
		}
		manifest, err := row.manifest()
		if err != nil {
			return nil, err
		}
		if err = appendArchivedArtifacts(artifacts, row.id, manifest.Artifacts); err != nil {
			return nil, err
		}
	}
	return artifacts, rows.Err()
}

func (row *archivedReleaseRow) scan(rows *sql.Rows) error {
	return rows.Scan(&row.id, &row.version, &row.channel, &row.keyID, &row.payload, &row.payloadBytes, &row.signatureBytes, &row.expires, &row.trustExists, &row.verified)
}

func (row archivedReleaseRow) manifest() (protocol.Manifest, error) {
	manifest, err := protocol.DecodeManifest(row.payload)
	if err != nil {
		return manifest, errAgentReleaseArchive
	}
	if row.signatureBytes != ed25519.SignatureSize || row.trustExists != 1 || (row.verified != 0 && row.verified != 1) {
		return manifest, errAgentReleaseArchive
	}
	if !row.matches(manifest) {
		return manifest, errAgentReleaseArchive
	}
	// 这里只校验归档结构；历史过期/撤销不丢弃，也不使用归档trust执行运行时验签。
	if !validArchivedTransition(manifest.NextKey) {
		return manifest, errAgentReleaseArchive
	}
	structural := manifest
	structural.Revoked = false
	if err = protocol.ValidateManifest(structural, structural.BuiltAt); err != nil {
		return manifest, errAgentReleaseArchive
	}
	return manifest, nil
}

func appendArchivedArtifacts(target map[string]model.BackupAgentReleaseArtifact, releaseID string, artifacts []protocol.Artifact) error {
	if len(target)+len(artifacts) > maximumArchivedAgentArtifacts {
		return errAgentReleaseArchive
	}
	for _, artifact := range artifacts {
		value := model.BackupAgentReleaseArtifact{ReleaseID: releaseID, ArtifactID: artifact.ID, SHA256: artifact.SHA256, Size: artifact.Size}
		key := agentArtifactKey(value)
		if _, exists := target[key]; exists {
			return errAgentReleaseArchive
		}
		target[key] = value
	}
	return nil
}

func validArchivedTransition(next *protocol.KeyTransition) bool {
	if next == nil {
		return true
	}
	return protocol.ValidSigningKeyID(next.ID) && len(next.PublicKey) == ed25519.PublicKeySize && len(next.Signature) == ed25519.SignatureSize && next.ExpiresAt.After(next.NotBefore)
}

func (row archivedReleaseRow) matches(manifest protocol.Manifest) bool {
	return row.id == protocol.ManifestDigest(row.payload) && manifest.Version == row.version && manifest.Channel == row.channel && manifest.SigningKeyID == row.keyID && manifest.ExpiresAt.Equal(row.expires)
}
