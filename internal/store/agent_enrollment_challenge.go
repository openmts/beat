package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentenrollment"
)

// 挑战存储错误：未知、已消费、已过期与绑定值不符对外只暴露同一个不可用错误。
var (
	ErrEnrollmentChallengeUnavailable = errors.New("enrollment challenge is not available")
	ErrEnrollmentChallengeLimit       = errors.New("enrollment challenge limit reached")
)

// maxActiveChallengesPerGrant 限定同一授权同时存在的未消费挑战数量。
const maxActiveChallengesPerGrant = 256

// EnrollmentChallenge 是一次性挑战的落库形态；明文挑战只出现一次，库内只有哈希。
type EnrollmentChallenge struct {
	ID             string
	GrantID        string
	KeyFingerprint []byte
	MetadataDigest []byte
	SourceIP       string
	ExpiresAt      time.Time
}

// EnrollmentChallengeInput 是创建挑战所需的全部输入。
type EnrollmentChallengeInput struct {
	GrantID        string
	ChallengeHash  []byte
	KeyFingerprint []byte
	MetadataDigest []byte
	SourceIP       string
	Now            time.Time
	ExpiresAt      time.Time
}

// EnrollmentChallengeProof 是消费挑战时 Agent 出示的绑定值。
type EnrollmentChallengeProof struct {
	ID             string
	Challenge      []byte
	KeyFingerprint []byte
	MetadataDigest []byte
	SourceIP       string
	Now            time.Time
}

// CreateChallenge 写入一次性挑战；过期挑战先被清理，超出上限直接拒绝。
func (s *AgentEnrollmentStore) CreateChallenge(ctx context.Context, input EnrollmentChallengeInput) (EnrollmentChallenge, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("create enrollment challenge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := pruneGrantChallenges(ctx, tx, input.GrantID, input.Now); err != nil {
		return EnrollmentChallenge{}, err
	}
	active, err := countActiveChallenges(ctx, tx, input.GrantID, input.Now)
	if err != nil {
		return EnrollmentChallenge{}, err
	}
	if active >= maxActiveChallengesPerGrant {
		return EnrollmentChallenge{}, ErrEnrollmentChallengeLimit
	}
	id := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_enrollment_challenges
		(id, grant_id, challenge_hash, key_fingerprint, metadata_digest, source_ip, expires_at, created_at, consumed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		id, input.GrantID, input.ChallengeHash, input.KeyFingerprint, input.MetadataDigest,
		input.SourceIP, input.ExpiresAt.UTC(), input.Now.UTC()); err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("create enrollment challenge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("create enrollment challenge: %w", err)
	}
	return EnrollmentChallenge{ID: id, GrantID: input.GrantID, KeyFingerprint: input.KeyFingerprint,
		MetadataDigest: input.MetadataDigest, SourceIP: input.SourceIP, ExpiresAt: input.ExpiresAt.UTC()}, nil
}

// ConsumeChallenge 原子消费挑战并校验全部绑定值；成功后再由调用方校验签名。
func (s *AgentEnrollmentStore) ConsumeChallenge(ctx context.Context, proof EnrollmentChallengeProof) (EnrollmentChallenge, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("consume enrollment challenge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	challenge, hash, err := loadChallenge(ctx, tx, proof.ID)
	if err != nil {
		return EnrollmentChallenge{}, err
	}
	if !bindingsMatch(challenge, hash, proof) {
		return EnrollmentChallenge{}, fmt.Errorf("consume enrollment challenge: %w", ErrEnrollmentChallengeUnavailable)
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_challenges SET consumed_at = ?
		WHERE id = ? AND consumed_at IS NULL`, proof.Now.UTC(), proof.ID)
	if err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("consume enrollment challenge: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return EnrollmentChallenge{}, fmt.Errorf("consume enrollment challenge: %w", ErrEnrollmentChallengeUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("consume enrollment challenge: %w", err)
	}
	return challenge, nil
}

func loadChallenge(ctx context.Context, tx *sql.Tx, id string) (EnrollmentChallenge, []byte, error) {
	var challenge EnrollmentChallenge
	var hash []byte
	var consumedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id, grant_id, challenge_hash, key_fingerprint, metadata_digest,
		source_ip, expires_at, consumed_at FROM agent_enrollment_challenges WHERE id = ?`, id).
		Scan(&challenge.ID, &challenge.GrantID, &hash, &challenge.KeyFingerprint, &challenge.MetadataDigest,
			&challenge.SourceIP, &challenge.ExpiresAt, &consumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EnrollmentChallenge{}, nil, fmt.Errorf("load enrollment challenge: %w", ErrEnrollmentChallengeUnavailable)
	}
	if err != nil {
		return EnrollmentChallenge{}, nil, fmt.Errorf("load enrollment challenge: %w", err)
	}
	return challenge, hash, nil
}

// bindingsMatch 逐项比对消费证明：过期、已消费、地址或摘要不符都归为同一个失败。
func bindingsMatch(challenge EnrollmentChallenge, hash []byte, proof EnrollmentChallengeProof) bool {
	if !challenge.ExpiresAt.After(proof.Now.UTC()) {
		return false
	}
	digest := agentenrollment.Digest(proof.Challenge)
	if subtle.ConstantTimeCompare(hash, digest[:]) != 1 {
		return false
	}
	if subtle.ConstantTimeCompare(challenge.KeyFingerprint, proof.KeyFingerprint) != 1 {
		return false
	}
	if subtle.ConstantTimeCompare(challenge.MetadataDigest, proof.MetadataDigest) != 1 {
		return false
	}
	return challenge.SourceIP == proof.SourceIP
}

func pruneGrantChallenges(ctx context.Context, tx *sql.Tx, grantID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_enrollment_challenges
		WHERE grant_id = ? AND (expires_at <= ? OR consumed_at IS NOT NULL)`, grantID, now.UTC()); err != nil {
		return fmt.Errorf("prune enrollment challenges: %w", err)
	}
	return nil
}

func countActiveChallenges(ctx context.Context, tx *sql.Tx, grantID string, now time.Time) (int, error) {
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_enrollment_challenges
		WHERE grant_id = ? AND consumed_at IS NULL AND expires_at > ?`, grantID, now.UTC()).Scan(&active); err != nil {
		return 0, fmt.Errorf("count enrollment challenges: %w", err)
	}
	return active, nil
}
