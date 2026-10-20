package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/agentjoin"
)

type enrollmentIdentity struct {
	ID, Name, Host       string
	Port                 int
	Hash                 []byte
	CreatedAt, RevokedAt sql.NullTime
}

type enrollmentBinding struct {
	nodeID    sql.NullString
	createdAt sql.NullTime
	known     bool
}

func findEnrollmentIdentity(ctx context.Context, db schemaExecutor, request agentjoin.Request) (*enrollmentIdentity, error) {
	hash := agentcredential.Digest(request.AgentToken)
	binding, err := readEnrollmentBinding(ctx, db, hash[:])
	if err != nil {
		return nil, err
	}
	var node enrollmentIdentity
	err = db.QueryRowContext(ctx, `SELECT id, name, host, port, agent_token_created_at, agent_token_revoked_at
		FROM nodes WHERE agent_token_hash = ?`, hash[:]).Scan(&node.ID, &node.Name, &node.Host, &node.Port, &node.CreatedAt, &node.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if binding.known {
			return nil, ErrEnrollmentIdentityUnavailable
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find enrolled identity: %w", err)
	}
	if node.RevokedAt.Valid || !node.CreatedAt.Valid {
		return nil, ErrEnrollmentIdentityUnavailable
	}
	if binding.known && (!binding.nodeID.Valid || binding.nodeID.String != node.ID || !binding.createdAt.Time.Equal(node.CreatedAt.Time)) {
		return nil, ErrEnrollmentIdentityUnavailable
	}
	request.NodeName, request.AdvertisedHost, request.SSHPort = node.Name, node.Host, node.Port
	if err := request.Validate(); err != nil {
		return nil, ErrEnrollmentIdentityUnavailable
	}
	node.Hash = hash[:]
	return &node, nil
}

func readEnrollmentBinding(ctx context.Context, db schemaExecutor, hash []byte) (enrollmentBinding, error) {
	var binding enrollmentBinding
	err := db.QueryRowContext(ctx, `SELECT node_id, credential_created_at FROM agent_enrollment_identities WHERE agent_token_hash = ?`, hash).
		Scan(&binding.nodeID, &binding.createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return binding, nil
	}
	if err != nil {
		return binding, fmt.Errorf("read enrollment identity binding: %w", err)
	}
	binding.known = true
	return binding, nil
}

func createEnrollmentNode(ctx context.Context, db schemaExecutor, attempt enrollmentAttempt) (*enrollmentIdentity, error) {
	groupID, err := defaultGroupIDFrom(ctx, db)
	if err != nil {
		return nil, err
	}
	request, now := attempt.input.Request, attempt.now
	hash := agentcredential.Digest(request.AgentToken)
	prefix, _ := agentcredential.DisplayPrefix(request.AgentToken)
	node := &enrollmentIdentity{ID: uuid.NewString(), Name: request.NodeName, Host: request.AdvertisedHost,
		Port: request.SSHPort, Hash: hash[:], CreatedAt: sql.NullTime{Time: now, Valid: true}}
	_, err = db.ExecContext(ctx, `INSERT INTO nodes (id, name, alias, group_id, host, port, status,
		ssh_public_key, agent_token_hash, agent_token_prefix, agent_token_created_at, credential_state, created_at, updated_at)
		VALUES (?, ?, '', ?, ?, ?, 'offline', '', ?, ?, ?, ?, ?, ?)`, node.ID, node.Name,
		groupID, node.Host, node.Port, node.Hash, prefix, now, string(agentenrollment.CredentialActive), now, now)
	if err != nil {
		return nil, mapNodeConstraintError("create enrolled node", err)
	}
	return node, nil
}

func rememberEnrollmentIdentity(ctx context.Context, db schemaExecutor, node enrollmentIdentity) error {
	_, err := db.ExecContext(ctx, `INSERT INTO agent_enrollment_identities (agent_token_hash, node_id, credential_created_at)
		VALUES (?, ?, ?) ON CONFLICT(agent_token_hash) DO NOTHING`, node.Hash, node.ID, node.CreatedAt.Time)
	if err != nil {
		return fmt.Errorf("remember enrollment identity: %w", err)
	}
	return nil
}
