package remoteoperations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/beat/backend/internal/agentcredential"
)

const CapabilityLifetime = 5 * time.Minute

func (tx transaction) node(proof NodeProof) (string, error) {
	if _, valid := agentcredential.DisplayPrefix(proof.Token); !valid {
		return "", ErrForbidden
	}
	digest := agentcredential.Digest(proof.Token)
	var id string
	err := tx.tx.QueryRowContext(tx.ctx, `SELECT id FROM nodes WHERE agent_token_hash=? AND
		agent_token_created_at IS NOT NULL AND agent_token_revoked_at IS NULL`, digest[:]).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", ErrUnavailable
	}
	return id, nil
}

func (service *Service) Advertise(ctx context.Context, proof NodeProof, policy Policy) (string, error) {
	if policy.Validate() != nil {
		return "", ErrInvalid
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return "", ErrInvalid
	}
	hash := sha256.Sum256(encoded)
	digest := hex.EncodeToString(hash[:])
	err = service.write(ctx, func(tx transaction) error {
		id, err := tx.node(proof)
		if err != nil {
			return err
		}
		return tx.one(`INSERT INTO operation_capabilities(node_id,digest,configuration_json,seen_at) VALUES(?,?,?,?)
			ON CONFLICT(node_id) DO UPDATE SET digest=excluded.digest,configuration_json=excluded.configuration_json,seen_at=excluded.seen_at`, id, digest, string(encoded), tx.now.UnixMilli())
	})
	return digest, err
}

func (tx transaction) capability(nodeID string) (Policy, string, error) {
	var encoded, digest string
	var seen int64
	err := tx.tx.QueryRowContext(tx.ctx, "SELECT digest,configuration_json,seen_at FROM operation_capabilities WHERE node_id=?", nodeID).Scan(&digest, &encoded, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{}, "", ErrForbidden
	}
	if err != nil {
		return Policy{}, "", ErrUnavailable
	}
	var policy Policy
	if tx.now.Sub(time.UnixMilli(seen)) > CapabilityLifetime || json.Unmarshal([]byte(encoded), &policy) != nil || policy.Validate() != nil {
		return Policy{}, "", ErrForbidden
	}
	return policy, digest, nil
}
