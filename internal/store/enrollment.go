package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentjoin"
)

var (
	ErrEnrollmentUnauthorized        = errors.New("join credential is invalid, expired, revoked or already used")
	ErrEnrollmentCapacity            = errors.New("join credential capacity reached")
	ErrEnrollmentSource              = errors.New("client source address is invalid")
	ErrEnrollmentIdentityUnavailable = errors.New(agentjoin.AgentIdentityUnavailableMessage)
)

const maximumEnrollmentTokens = 4096

type EnrollmentTokenInput struct {
	Hash      []byte
	Prefix    string
	ServerURL string
	AllowHTTP bool
	CreatorID string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type EnrollmentToken struct {
	ID        string
	ServerURL string
	ExpiresAt time.Time
}

type EnrollmentConsumeInput struct {
	TokenHash      []byte
	Request        agentjoin.Request
	SourceIP       string
	Secure         bool
	Now            func() time.Time
	AuditRequestID string
}

func (s *NodeStore) CreateEnrollmentToken(ctx context.Context, input EnrollmentTokenInput) (EnrollmentToken, error) {
	origin, err := agentjoin.NormalizeServerURL(input.ServerURL, input.AllowHTTP)
	if err != nil || len(input.Hash) != sha256.Size || input.CreatorID == "" || !input.ExpiresAt.After(input.CreatedAt) {
		return EnrollmentToken{}, errors.New("invalid join credential metadata")
	}
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentToken{}, fmt.Errorf("begin enrollment issuance: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := reserveEnrollmentCapacity(ctx, tx, input.CreatedAt); err != nil {
		return EnrollmentToken{}, err
	}
	result := EnrollmentToken{ID: uuid.NewString(), ServerURL: origin, ExpiresAt: input.ExpiresAt.UTC()}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_enrollment_tokens
		(id, token_hash, token_prefix, server_url, allow_http, creator_id, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, result.ID, input.Hash, input.Prefix, origin,
		input.AllowHTTP, input.CreatorID, input.CreatedAt.UTC(), result.ExpiresAt)
	if err != nil {
		return EnrollmentToken{}, fmt.Errorf("create enrollment credential: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentToken{}, fmt.Errorf("commit enrollment issuance: %w", err)
	}
	return result, nil
}

func reserveEnrollmentCapacity(ctx context.Context, db schemaExecutor, now time.Time) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM agent_enrollment_tokens WHERE expires_at <= ?", now.UTC()); err != nil {
		return fmt.Errorf("prune enrollment credentials: %w", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_enrollment_tokens").Scan(&count); err != nil {
		return fmt.Errorf("count enrollment credentials: %w", err)
	}
	if count >= maximumEnrollmentTokens {
		return ErrEnrollmentCapacity
	}
	return nil
}

func (s *NodeStore) RevokeEnrollmentToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE agent_enrollment_tokens SET revoked_at = ? WHERE id = ?", time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("revoke enrollment credential: %w", err)
	}
	return nil
}
