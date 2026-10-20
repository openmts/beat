package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

// 清理任务的原因码与操作者标记：过期与授权关闭都不恢复已消耗的名额。
const (
	enrollmentReasonRequestExpired = "request_expired"
	enrollmentReasonGrantClosed    = "grant_closed"
	enrollmentSystemActor          = "system:enrollment-cleanup"
)

// EnrollmentCleanupResult 汇总一次清理的结果。
type EnrollmentCleanupResult struct {
	ExpiredRequests  int
	PrunedChallenges int
	PrunedTickets    int
}

// enrollmentOpenRequestCondition 覆盖两类必须终结的未认领请求：自身超时与授权关闭。
const enrollmentOpenRequestCondition = `r.status IN ('pending', 'approved') AND (r.request_expires_at <= ?
	OR EXISTS(SELECT 1 FROM agent_enrollment_grants g WHERE g.id = r.grant_id
		AND (g.revoked_at IS NOT NULL OR g.expires_at <= ?)))`

// CleanupEnrollmentState 在单个事务里终结过期请求、清理票据与挑战，并删除无人持有的保留节点。
func (s *AgentEnrollmentStore) CleanupEnrollmentState(ctx context.Context, now time.Time) (EnrollmentCleanupResult, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentCleanupResult{}, fmt.Errorf("cleanup enrollment state: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result := EnrollmentCleanupResult{}
	if result.PrunedChallenges, err = pruneExpiredChallenges(ctx, tx, now.UTC()); err != nil {
		return EnrollmentCleanupResult{}, err
	}
	if result.PrunedTickets, err = pruneExpiredTicketsTx(ctx, tx, now.UTC()); err != nil {
		return EnrollmentCleanupResult{}, err
	}
	if result.ExpiredRequests, err = expireOpenEnrollmentRequests(ctx, tx, now.UTC()); err != nil {
		return EnrollmentCleanupResult{}, err
	}
	if err := deleteOrphanEnrollmentNodes(ctx, tx); err != nil {
		return EnrollmentCleanupResult{}, err
	}
	if err := writeEnrollmentExpiryAudit(ctx, tx, result, now); err != nil {
		return EnrollmentCleanupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentCleanupResult{}, fmt.Errorf("cleanup enrollment state: %w", err)
	}
	return result, nil
}

// writeEnrollmentExpiryAudit 只在真的终结了请求时留痕；干净的清理回合不写审计行。
func writeEnrollmentExpiryAudit(ctx context.Context, tx *sql.Tx, result EnrollmentCleanupResult, now time.Time) error {
	if result.ExpiredRequests == 0 {
		return nil
	}
	return writeEnrollmentAudit(ctx, tx, enrollmentRequestAuditEvent(enrollmentRequestAuditInput{
		Action: EnrollmentActionRequestExpire, Detail: map[string]any{
			"expired_requests": result.ExpiredRequests, "pruned_challenges": result.PrunedChallenges,
			"pruned_tickets": result.PrunedTickets}, Now: now}))
}

func pruneExpiredChallenges(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	result, err := tx.ExecContext(ctx, `DELETE FROM agent_enrollment_challenges
		WHERE expires_at <= ? OR consumed_at IS NOT NULL`, now)
	if err != nil {
		return 0, fmt.Errorf("prune enrollment challenges: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune enrollment challenges: %w", err)
	}
	return int(affected), nil
}

func pruneExpiredTicketsTx(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	result, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests
		SET ticket_hash = NULL, ticket_expires_at = NULL
		WHERE ticket_hash IS NOT NULL AND ticket_expires_at IS NOT NULL AND ticket_expires_at <= ?`, now)
	if err != nil {
		return 0, fmt.Errorf("prune enrollment tickets: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune enrollment tickets: %w", err)
	}
	return int(affected), nil
}

// expireOpenEnrollmentRequests 保留名称快照、删除隐藏节点并把请求置为终态。
func expireOpenEnrollmentRequests(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	if err := snapshotEnrollmentNodeNames(ctx, tx, now); err != nil {
		return 0, err
	}
	if err := deleteExpiredReservationNodes(ctx, tx, now); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests AS r SET status = 'expired',
		rejected_by = ?, rejected_at = ?, rejection_reason = ?, ticket_hash = NULL, ticket_expires_at = NULL,
		recovery_deadline = NULL, reserved_node_id = NULL WHERE `+enrollmentOpenRequestCondition,
		enrollmentSystemActor, now, enrollmentReasonRequestExpired, now, now)
	if err != nil {
		return 0, fmt.Errorf("expire enrollment requests: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire enrollment requests: %w", err)
	}
	return int(affected), nil
}

func snapshotEnrollmentNodeNames(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests AS r SET node_name_snapshot =
		COALESCE((SELECT n.name FROM nodes n WHERE n.id = r.reserved_node_id), r.node_name_snapshot)
		WHERE `+enrollmentOpenRequestCondition, now, now)
	if err != nil {
		return fmt.Errorf("snapshot enrollment node names: %w", err)
	}
	return nil
}

func deleteExpiredReservationNodes(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE credential_state = ?
		AND agent_token_hash IS NULL AND id IN (SELECT r.reserved_node_id FROM agent_enrollment_requests r
			WHERE r.reserved_node_id IS NOT NULL AND `+enrollmentOpenRequestCondition+`)`,
		string(agentenrollment.CredentialPendingClaim), now, now)
	if err != nil {
		return fmt.Errorf("delete expired enrollment nodes: %w", err)
	}
	return nil
}

// deleteOrphanEnrollmentNodes 兜底清理没有任何请求引用的隐藏节点。
func deleteOrphanEnrollmentNodes(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE credential_state = ? AND agent_token_hash IS NULL
		AND id NOT IN (SELECT reserved_node_id FROM agent_enrollment_requests WHERE reserved_node_id IS NOT NULL)`,
		string(agentenrollment.CredentialPendingClaim))
	if err != nil {
		return fmt.Errorf("delete orphan enrollment nodes: %w", err)
	}
	return nil
}
