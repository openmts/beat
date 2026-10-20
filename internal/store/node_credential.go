package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/model"
)

var (
	ErrNodeNameConflict   = errors.New("node name already exists")
	ErrAgentTokenConflict = errors.New("agent token already exists")
)

type ManagedNodeInput struct {
	Name         string
	Alias        string
	GroupID      string
	Host         string
	Port         int
	SSHPublicKey string
}

type AgentCredential struct {
	Hash      []byte
	Prefix    string
	CreatedAt time.Time
}

func (s *NodeStore) CreateManagedNode(
	ctx context.Context,
	input ManagedNodeInput,
	credential AgentCredential,
) (*model.Node, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("begin managed node creation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return nil, err
	}

	groupID := input.GroupID
	if groupID == "" {
		if err := tx.QueryRowContext(ctx,
			"SELECT id FROM groups WHERE is_default = 1 LIMIT 1",
		).Scan(&groupID); err != nil {
			return nil, fmt.Errorf("query managed node default group: %w", err)
		}
	}
	now := credential.CreatedAt.UTC()
	id := uuid.NewString()
	_, err = tx.ExecContext(ctx, "INSERT INTO nodes ("+
		"id, name, alias, group_id, host, port, status, ssh_public_key, "+
		"agent_token_hash, agent_token_prefix, agent_token_created_at, credential_state, created_at, updated_at"+
		") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, input.Name, input.Alias, groupID, input.Host, input.Port, model.NodeStatusOffline,
		input.SSHPublicKey, credential.Hash, credential.Prefix, now, string(agentenrollment.CredentialActive), now, now,
	)
	if err != nil {
		return nil, mapNodeConstraintError("insert managed node", err)
	}
	if err := guardAlertScopeMutation(ctx, tx); err != nil {
		return nil, err
	}
	return commitCredentialNode(ctx, tx, credentialCommit{id: id, mutation: mutation, action: "node.create", verify: credentialVerifier(credential)})
}

func (s *NodeStore) RotateAgentToken(
	ctx context.Context,
	nodeID string,
	credential AgentCredential,
) (*model.Node, error) {
	return s.mutateCredentialNode(ctx, credentialMutation{id: nodeID, action: "node.token.rotate", verify: credentialVerifier(credential), apply: func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "UPDATE nodes SET agent_token_hash = ?, agent_token_prefix = ?, "+
			"agent_token_created_at = ?, agent_token_last_used_at = NULL, agent_token_revoked_at = NULL, "+
			"updated_at = ? WHERE id = ?", credential.Hash, credential.Prefix, credential.CreatedAt.UTC(),
			credential.CreatedAt.UTC(), nodeID)
		if err != nil {
			return mapNodeConstraintError("rotate agent token", err)
		}
		return sessionRowsAffected(result, err, "rotate agent token")
	}})
}

func (s *NodeStore) RevokeAgentToken(
	ctx context.Context,
	nodeID string,
	revokedAt time.Time,
) (*model.Node, error) {
	return s.mutateCredentialNode(ctx, credentialMutation{id: nodeID, action: "node.token.revoke", verify: func(node model.Node) error {
		if node.AgentTokenRevokedAt == nil || !node.AgentTokenRevokedAt.Equal(revokedAt) {
			return errors.New("revocation was not persisted")
		}
		return nil
	}, apply: func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "UPDATE nodes SET agent_token_revoked_at = ?, updated_at = ? WHERE id = ?",
			revokedAt.UTC(), revokedAt.UTC(), nodeID)
		if err != nil {
			return fmt.Errorf("revoke agent token: %w", err)
		}
		return sessionRowsAffected(result, err, "revoke agent token")
	}})
}

func (s *NodeStore) AuthenticateAgentToken(ctx context.Context, token string) (*model.Node, error) {
	if _, valid := agentcredential.DisplayPrefix(token); !valid {
		return nil, nil
	}
	digest := agentcredential.Digest(token)
	node, err := scanNode(s.db.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+
		" FROM nodes WHERE agent_token_hash = ? AND agent_token_created_at IS NOT NULL"+
		" AND agent_token_revoked_at IS NULL AND lifecycle_state = 'active'"+
		" AND credential_state = 'active'", digest[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate agent token: %w", err)
	}
	return &node, nil
}

func (s *NodeStore) AuthenticateLegacyNode(ctx context.Context, name string) (*model.Node, error) {
	node, err := scanNode(s.db.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+
		" FROM nodes WHERE name = ? AND agent_token_hash IS NULL"+
		" AND agent_token_created_at IS NULL AND agent_token_revoked_at IS NULL"+
		" AND lifecycle_state = 'active' AND credential_state = 'legacy'", name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate legacy node: %w", err)
	}
	return &node, nil
}

func (s *NodeStore) TouchAgentToken(
	ctx context.Context,
	nodeID string,
	usedAt time.Time,
	minimumInterval time.Duration,
) error {
	cutoff := usedAt.Add(-minimumInterval).UTC()
	_, err := s.db.ExecContext(ctx, "UPDATE nodes SET agent_token_last_used_at = ? "+
		"WHERE id = ? AND agent_token_hash IS NOT NULL AND agent_token_revoked_at IS NULL "+
		"AND (agent_token_last_used_at IS NULL OR agent_token_last_used_at <= ?)",
		usedAt.UTC(), nodeID, cutoff)
	if err != nil {
		return fmt.Errorf("touch agent token: %w", err)
	}
	return nil
}

func mapNodeConstraintError(operation string, err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "nodes.name"):
		return fmt.Errorf("%s: %w", operation, ErrNodeNameConflict)
	case strings.Contains(message, "nodes.agent_token_hash"):
		return fmt.Errorf("%s: %w", operation, ErrAgentTokenConflict)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

type credentialMutation struct {
	id, action string
	apply      func(*sql.Tx) error
	verify     func(model.Node) error
}

type credentialCommit struct {
	id, action string
	mutation   metadataMutation
	verify     func(model.Node) error
}

func (s *NodeStore) mutateCredentialNode(ctx context.Context, input credentialMutation) (*model.Node, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return nil, err
	}
	previous, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+" FROM nodes WHERE id=?", input.id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := input.apply(tx); err != nil {
		return nil, err
	}
	node, err := commitCredentialNode(ctx, tx, credentialCommit{id: input.id, action: input.action, mutation: mutation, verify: input.verify})
	if err == nil && node != nil && s.credentialInvalidator != nil {
		s.credentialInvalidator(previous.ID, nodeCredentialBinding(previous))
	}
	return node, err
}

func commitCredentialNode(ctx context.Context, tx *sql.Tx, input credentialCommit) (*model.Node, error) {
	node, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeSelectColumns+" FROM nodes WHERE id=?", input.id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prepare credential response: %w", err)
	}
	if input.verify != nil {
		if err := input.verify(node); err != nil {
			return nil, err
		}
	}
	if err := input.mutation.finish(metadataEvidence{Action: input.action, Type: "node", ID: node.ID, Name: node.Name, Fields: []string{"agent_credential"}}); err != nil {
		return nil, err
	}
	if err := verifyNodeSnapshot(ctx, tx, node); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit node credential mutation: %w", err)
	}
	return &node, nil
}

func credentialVerifier(expected AgentCredential) func(model.Node) error {
	return func(node model.Node) error {
		if !bytes.Equal(node.AgentTokenHash, expected.Hash) || node.AgentTokenPrefix != expected.Prefix || node.AgentTokenCreatedAt == nil || !node.AgentTokenCreatedAt.Equal(expected.CreatedAt) || node.AgentTokenRevokedAt != nil {
			return errors.New("credential was not persisted")
		}
		return nil
	}
}
