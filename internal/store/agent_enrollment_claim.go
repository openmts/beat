package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
)

// 认领相关的存储错误：票据与恢复窗口分开暴露，便于 Agent 区分重试与人工介入。
var (
	ErrEnrollmentTicketUnavailable   = errors.New("enrollment claim ticket is not available")
	ErrEnrollmentRecoveryUnavailable = errors.New("enrollment claim recovery is not available")
)

// enrollmentTicketTTL 是认领票据的有效期；每次状态轮询都会轮换。
const enrollmentTicketTTL = 2 * time.Minute

// EnrollmentClaim 是 Agent 兑换节点凭据时提交的票据。
type EnrollmentClaim struct {
	RequestID string
	Ticket    string
}

// EnrollmentClaimResult 是认领成功后的节点凭据与节点字段。
type EnrollmentClaimResult struct {
	Request   EnrollmentRequest
	NodeID    string
	NodeName  string
	Host      string
	Port      int
	GroupID   string
	Token     agentcredential.Token
	Recovered bool
}

// RotateEnrollmentTicket 为一次状态轮询轮换票据，generation 单调递增。
func (s *AgentEnrollmentStore) RotateEnrollmentTicket(ctx context.Context, id string,
	ticket agentenrollment.Secret, now time.Time) (EnrollmentRequest, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE agent_enrollment_requests
		SET ticket_hash = ?, ticket_expires_at = ?, ticket_generation = ticket_generation + 1
		WHERE id = ? AND status IN ('approved', 'claimed')`,
		ticket.Hash, now.UTC().Add(enrollmentTicketTTL), id)
	if err != nil {
		return EnrollmentRequest{}, fmt.Errorf("rotate enrollment ticket: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return EnrollmentRequest{}, fmt.Errorf("rotate enrollment ticket: %w", err)
	}
	if affected == 0 {
		return EnrollmentRequest{}, ErrEnrollmentTicketUnavailable
	}
	request, err := s.EnrollmentRequest(ctx, id)
	if err != nil {
		return EnrollmentRequest{}, err
	}
	return *request, nil
}

// ClaimEnrollmentRequest 兑换节点令牌；恢复路径会轮换令牌并消耗一次恢复额度。
func (s *AgentEnrollmentStore) ClaimEnrollmentRequest(ctx context.Context, input EnrollmentClaim,
	token agentcredential.Token, now time.Time) (EnrollmentClaimResult, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentClaimResult{}, fmt.Errorf("claim enrollment request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	request, err := loadEnrollmentRequestTx(ctx, tx, input.RequestID)
	if err != nil {
		return EnrollmentClaimResult{}, err
	}
	if err := requireEnrollmentTicket(*request, input.Ticket, now.UTC()); err != nil {
		return EnrollmentClaimResult{}, err
	}
	grant, err := enrollmentGrantForUpdate(ctx, tx, request.GrantID)
	if err != nil {
		return EnrollmentClaimResult{}, err
	}
	if !grant.enrollmentActive(now.UTC()) {
		return EnrollmentClaimResult{}, fmt.Errorf("claim enrollment request: %w", ErrEnrollmentGrantUnavailable)
	}
	result, err := applyEnrollmentClaim(ctx, tx, *request, *grant, token, now.UTC())
	if err != nil {
		return EnrollmentClaimResult{}, err
	}
	if err := writeEnrollmentAudit(ctx, tx, claimAudit(ctx, input, result, now)); err != nil {
		return EnrollmentClaimResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentClaimResult{}, fmt.Errorf("claim enrollment request: %w", err)
	}
	return result, nil
}

// claimAudit 记录一次凭据认领；明细只含节点标识与是否走恢复路径。
func claimAudit(ctx context.Context, input EnrollmentClaim, result EnrollmentClaimResult, now time.Time) enrollmentAuditEvent {
	action := EnrollmentActionClaim
	if result.Recovered {
		action = EnrollmentActionClaimRecover
	}
	return enrollmentRequestAuditEvent(enrollmentRequestAuditInput{Action: action, RequestID: input.RequestID,
		Audit: enrollmentAuditFrom(ctx), Now: now, Detail: map[string]any{"node_id": result.NodeID, "recovered": result.Recovered}})
}

// applyEnrollmentClaim 分派首次认领与有界恢复两条路径。
func applyEnrollmentClaim(ctx context.Context, tx *sql.Tx, request EnrollmentRequest, grant EnrollmentGrant,
	token agentcredential.Token, now time.Time) (EnrollmentClaimResult, error) {
	switch request.Status {
	case agentenrollment.RequestApproved:
		return activateEnrollmentNode(ctx, tx, request, token, now)
	case agentenrollment.RequestClaimed:
		return recoverEnrollmentNode(ctx, tx, request, grant, token, now)
	default:
		return EnrollmentClaimResult{}, fmt.Errorf("claim enrollment request: %w", ErrEnrollmentRequestUnavailable)
	}
}

// activateEnrollmentNode 把保留节点转为 active 并写入一次性节点令牌。
func activateEnrollmentNode(ctx context.Context, tx *sql.Tx, request EnrollmentRequest,
	token agentcredential.Token, now time.Time) (EnrollmentClaimResult, error) {
	node, err := reservedEnrollmentNode(ctx, tx, request)
	if err != nil {
		return EnrollmentClaimResult{}, err
	}
	if err := writeEnrollmentNodeToken(ctx, tx, node.id, token, now); err != nil {
		return EnrollmentClaimResult{}, err
	}
	if err := rememberEnrollmentIdentity(ctx, tx, enrollmentIdentity{
		ID: node.id, Hash: token.Hash, CreatedAt: sql.NullTime{Time: now, Valid: true},
	}); err != nil {
		return EnrollmentClaimResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET status = 'claimed',
		claimed_at = ?, ticket_hash = NULL, ticket_expires_at = NULL, recovery_deadline = ?,
		recovery_attempts = 0 WHERE id = ? AND status = 'approved'`,
		now, now.Add(agentenrollment.ClaimRecoveryWindow), request.ID); err != nil {
		return EnrollmentClaimResult{}, fmt.Errorf("claim enrollment request: %w", err)
	}
	return EnrollmentClaimResult{Request: request, NodeID: node.id, NodeName: node.name, Host: node.host,
		Port: node.port, GroupID: node.groupID, Token: token, Recovered: false}, nil
}

// recoverEnrollmentNode 在受限窗口内轮换节点令牌，使可能已泄漏的旧令牌立即失效。
func recoverEnrollmentNode(ctx context.Context, tx *sql.Tx, request EnrollmentRequest, grant EnrollmentGrant,
	token agentcredential.Token, now time.Time) (EnrollmentClaimResult, error) {
	if !grant.ClaimRecovery || !request.RecoveryDeadline.Valid || !now.Before(request.RecoveryDeadline.Time) ||
		request.RecoveryAttempts >= agentenrollment.MaxClaimRecoveryAttempts || !request.ReservedNodeID.Valid {
		return EnrollmentClaimResult{}, ErrEnrollmentRecoveryUnavailable
	}
	node, err := activeEnrollmentNode(ctx, tx, request.ReservedNodeID.String)
	if err != nil {
		return EnrollmentClaimResult{}, err
	}
	if err := writeEnrollmentNodeToken(ctx, tx, node.id, token, now); err != nil {
		return EnrollmentClaimResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET ticket_hash = NULL,
		ticket_expires_at = NULL, recovery_attempts = recovery_attempts + 1 WHERE id = ? AND status = 'claimed'`,
		request.ID); err != nil {
		return EnrollmentClaimResult{}, fmt.Errorf("recover enrollment claim: %w", err)
	}
	return EnrollmentClaimResult{Request: request, NodeID: node.id, NodeName: node.name, Host: node.host,
		Port: node.port, GroupID: node.groupID, Token: token, Recovered: true}, nil
}

type enrollmentNodeRow struct {
	id, name, host, groupID string
	port                    int
}

func reservedEnrollmentNode(ctx context.Context, tx *sql.Tx, request EnrollmentRequest) (enrollmentNodeRow, error) {
	if !request.ReservedNodeID.Valid {
		return enrollmentNodeRow{}, fmt.Errorf("claim enrollment request: %w", ErrEnrollmentRequestUnavailable)
	}
	return readEnrollmentNode(ctx, tx, request.ReservedNodeID.String, string(agentenrollment.CredentialPendingClaim))
}

func activeEnrollmentNode(ctx context.Context, tx *sql.Tx, nodeID string) (enrollmentNodeRow, error) {
	return readEnrollmentNode(ctx, tx, nodeID, string(agentenrollment.CredentialActive))
}

func readEnrollmentNode(ctx context.Context, tx *sql.Tx, nodeID, state string) (enrollmentNodeRow, error) {
	var node enrollmentNodeRow
	err := tx.QueryRowContext(ctx, `SELECT id, name, host, port, group_id FROM nodes
		WHERE id = ? AND credential_state = ? AND lifecycle_state = 'active'`, nodeID, state).
		Scan(&node.id, &node.name, &node.host, &node.port, &node.groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return enrollmentNodeRow{}, fmt.Errorf("load enrollment node: %w", ErrEnrollmentRequestUnavailable)
	}
	if err != nil {
		return enrollmentNodeRow{}, fmt.Errorf("load enrollment node: %w", err)
	}
	return node, nil
}

func writeEnrollmentNodeToken(ctx context.Context, tx *sql.Tx, nodeID string, token agentcredential.Token,
	now time.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE nodes SET agent_token_hash = ?, agent_token_prefix = ?,
		agent_token_created_at = ?, agent_token_last_used_at = NULL, agent_token_revoked_at = NULL,
		credential_state = ?, updated_at = ? WHERE id = ?`,
		token.Hash, token.Prefix, now, string(agentenrollment.CredentialActive), now, nodeID)
	if err != nil {
		return mapNodeConstraintError("claim enrollment node token", err)
	}
	return nil
}

// requireEnrollmentTicket 校验票据明文、有效期与请求状态。
func requireEnrollmentTicket(request EnrollmentRequest, ticket string, now time.Time) error {
	if request.TicketHash == nil || !agentenrollment.SecretMatches(request.TicketHash, ticket) {
		return fmt.Errorf("claim enrollment request: %w", ErrEnrollmentTicketUnavailable)
	}
	if !request.TicketExpiresAt.Valid || !request.TicketExpiresAt.Time.After(now) {
		return fmt.Errorf("claim enrollment request: %w", ErrEnrollmentTicketUnavailable)
	}
	return nil
}

// enrollmentActive 判断授权是否仍允许认领；容量已满不影响已审批请求的认领。
func (grant EnrollmentGrant) enrollmentActive(now time.Time) bool {
	return grant.Enabled && !grant.RevokedAt.Valid && grant.ExpiresAt.After(now)
}
