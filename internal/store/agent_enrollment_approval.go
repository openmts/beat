package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/model"
)

// 自动模式无法按策略审批时记录的原因码；请求保持 pending 等待人工复审。
const (
	enrollmentReasonPolicyConflict = "policy_conflict"
	enrollmentReasonNameConflict   = "name_conflict"
	maxEnrollmentNodeNameLength    = 256
	maxRejectionReasonLength       = 64
)

// 入网保留节点的凭据状态与上报状态；只有认领后才会写入节点令牌。
const (
	enrollmentNodeStatus = "offline"
)

// EnrollmentApproval 是一次审批决策：人工审批由管理端提供最终节点字段。
type EnrollmentApproval struct {
	RequestID string
	NodeName  string
	GroupID   string
	Tags      []string
	Remarks   string
	Public    bool
	Host      string
	SSHPort   int
	ActorID   string
}

// EnrollmentRejection 是一次拒绝决策。
type EnrollmentRejection struct {
	RequestID, ActorID, Reason string
}

// ApproveEnrollmentRequest 审批一条待处理请求，保留隐藏节点并占用一个授权名额。
func (s *AgentEnrollmentStore) ApproveEnrollmentRequest(ctx context.Context, input EnrollmentApproval, now time.Time) (EnrollmentRequest, error) {
	decision := enrollmentDecision{requestID: input.RequestID, audit: approvalAudit(ctx, now)}
	decision.decide = func(tx *sql.Tx, request EnrollmentRequest, grant EnrollmentGrant) error {
		name := strings.TrimSpace(input.NodeName)
		if err := validateEnrollmentNodeName(name); err != nil {
			return err
		}
		tags, err := model.NormalizeNodeTags(input.Tags)
		if err != nil {
			return fmt.Errorf("%w: %s", agentenrollment.ErrInvalidMetadata, err)
		}
		if input.SSHPort < agentenrollment.MinSSHPort || input.SSHPort > agentenrollment.MaxSSHPort {
			return fmt.Errorf("%w: SSH port must be %d-%d", agentenrollment.ErrInvalidMetadata,
				agentenrollment.MinSSHPort, agentenrollment.MaxSSHPort)
		}
		groupID := input.GroupID
		if strings.TrimSpace(groupID) == "" {
			groupID = grant.DefaultGroupID
		}
		// 审批人未填写地址时沿用服务端观测到的来源 IP，与一次性 join 的默认行为一致。
		host := strings.TrimSpace(input.Host)
		if host == "" {
			host = request.SourceIP
		}
		return reserveEnrollmentNode(ctx, tx, reservation{
			RequestID: request.ID, GrantID: grant.ID, Name: name, GroupID: groupID, Tags: tags,
			Remarks: input.Remarks, Public: input.Public, Host: host,
			SSHPort: input.SSHPort, ActorID: input.ActorID, Now: now.UTC(),
		})
	}
	return s.decideEnrollmentRequest(ctx, decision)
}

// approvalAudit 由审批后的请求生成审计行，节点 ID 与名称取自落库结果。
func approvalAudit(ctx context.Context, now time.Time) func(EnrollmentRequest) enrollmentAuditEvent {
	return func(updated EnrollmentRequest) enrollmentAuditEvent {
		return enrollmentRequestAuditEvent(enrollmentRequestAuditInput{Action: EnrollmentActionRequestApprove,
			RequestID: updated.ID, Audit: enrollmentAuditFrom(ctx), Now: now, Detail: map[string]any{
				"grant_id": updated.GrantID, "node_id": updated.ReservedNodeID.String,
				"node_name": updated.NodeNameSnapshot,
			}})
	}
}

// RejectEnrollmentRequest 拒绝一条未认领请求并清理隐藏节点保留。
func (s *AgentEnrollmentStore) RejectEnrollmentRequest(ctx context.Context, input EnrollmentRejection, now time.Time) (EnrollmentRequest, error) {
	decision := enrollmentDecision{requestID: input.RequestID, audit: rejectionAudit(ctx, now)}
	decision.decide = func(tx *sql.Tx, request EnrollmentRequest, _ EnrollmentGrant) error {
		reason := strings.TrimSpace(input.Reason)
		if len(reason) > maxRejectionReasonLength {
			return fmt.Errorf("%w: rejection reason is too long", agentenrollment.ErrInvalidMetadata)
		}
		result, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET status = 'rejected',
			rejected_by = ?, rejected_at = ?, rejection_reason = ?, ticket_hash = NULL, ticket_expires_at = NULL,
			recovery_deadline = NULL WHERE id = ? AND status IN ('pending', 'approved')`,
			input.ActorID, now.UTC(), reason, request.ID)
		if err != nil {
			return fmt.Errorf("reject enrollment request: %w", err)
		}
		return releaseEnrollmentReservation(ctx, tx, request.ID, result, err)
	}
	return s.decideEnrollmentRequest(ctx, decision)
}

// rejectionAudit 由拒绝后的请求生成审计行，只记录有界原因码。
func rejectionAudit(ctx context.Context, now time.Time) func(EnrollmentRequest) enrollmentAuditEvent {
	return func(updated EnrollmentRequest) enrollmentAuditEvent {
		return enrollmentRequestAuditEvent(enrollmentRequestAuditInput{Action: EnrollmentActionRequestReject,
			RequestID: updated.ID, Audit: enrollmentAuditFrom(ctx), Now: now, Detail: map[string]any{
				"grant_id": updated.GrantID, "reason": updated.RejectionReason}})
	}
}

// enrollmentDecision 是一次审批或拒绝：判定逻辑与审计行在同一事务内提交。
type enrollmentDecision struct {
	requestID string
	decide    func(*sql.Tx, EnrollmentRequest, EnrollmentGrant) error
	audit     func(EnrollmentRequest) enrollmentAuditEvent
}

// decideEnrollmentRequest 提供审批与拒绝共用的读取、状态校验与提交骨架。
func (s *AgentEnrollmentStore) decideEnrollmentRequest(ctx context.Context, decision enrollmentDecision) (EnrollmentRequest, error) {
	id := decision.requestID
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentRequest{}, fmt.Errorf("decide enrollment request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	request, err := loadEnrollmentRequestTx(ctx, tx, id)
	if err != nil {
		return EnrollmentRequest{}, err
	}
	grant, err := enrollmentGrantForUpdate(ctx, tx, request.GrantID)
	if err != nil {
		return EnrollmentRequest{}, err
	}
	if err := decision.decide(tx, *request, *grant); err != nil {
		return EnrollmentRequest{}, err
	}
	if err := writeDecisionAudit(ctx, tx, decision, id); err != nil {
		return EnrollmentRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentRequest{}, fmt.Errorf("decide enrollment request: %w", err)
	}
	updated, err := s.EnrollmentRequest(ctx, id)
	if err != nil {
		return EnrollmentRequest{}, err
	}
	return *updated, nil
}

// writeDecisionAudit 在事务内按决策后的最新状态写审计行；未配置时保持无操作。
func writeDecisionAudit(ctx context.Context, tx *sql.Tx, decision enrollmentDecision, id string) error {
	if decision.audit == nil {
		return nil
	}
	updated, err := loadEnrollmentRequestTx(ctx, tx, id)
	if err != nil {
		return err
	}
	return writeEnrollmentAudit(ctx, tx, decision.audit(*updated))
}

// reservation 描述一次审批要落库的隐藏节点。
type reservation struct {
	RequestID, GrantID, Name, GroupID, Remarks, Host, ActorID string
	Tags                                                      []string
	Public                                                    bool
	SSHPort                                                   int
	Now                                                       time.Time
}

// reserveEnrollmentNode 占用授权名额、写入 pending_claim 节点并把节点 ID 记到请求上。
func reserveEnrollmentNode(ctx context.Context, tx *sql.Tx, input reservation) error {
	result, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_grants
		SET consumed_enrollments = consumed_enrollments + 1, updated_at = ?
		WHERE id = ? AND revoked_at IS NULL AND consumed_enrollments < maximum_enrollments`,
		input.Now, input.GrantID)
	if err != nil {
		return fmt.Errorf("reserve enrollment capacity: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return fmt.Errorf("reserve enrollment capacity: %w", ErrEnrollmentGrantCapacity)
	}
	nodeID := uuid.NewString()
	if err := insertEnrollmentNode(ctx, tx, nodeID, input); err != nil {
		return err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET status = 'approved',
		reserved_node_id = ?, node_name_snapshot = ?, approved_by = ?, approved_at = ?, rejection_reason = ''
		WHERE id = ? AND status = 'pending'`, nodeID, input.Name, input.ActorID, input.Now, input.RequestID)
	if err != nil {
		return fmt.Errorf("approve enrollment request: %w", err)
	}
	return requireSingleRow(updated, err, input.RequestID)
}

func insertEnrollmentNode(ctx context.Context, tx *sql.Tx, nodeID string, input reservation) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO nodes (id, name, alias, group_id, host, port, status,
		ssh_public_key, agent_token_prefix, credential_state, tags, is_public, public_remark, private_remark,
		created_at, updated_at) VALUES (?, ?, '', ?, ?, ?, ?, '', '', ?, ?, ?, '', ?, ?, ?)`,
		nodeID, input.Name, input.GroupID, input.Host, input.SSHPort, enrollmentNodeStatus,
		string(agentenrollment.CredentialPendingClaim), encodeGrantList(input.Tags), boolToInt(input.Public),
		input.Remarks, input.Now, input.Now)
	if err != nil {
		return mapNodeConstraintError("reserve enrollment node", err)
	}
	return nil
}

// releaseEnrollmentReservation 清空请求的保留节点并删除从未持有令牌的隐藏节点。
func releaseEnrollmentReservation(ctx context.Context, tx *sql.Tx, requestID string, result sql.Result, err error) error {
	if err := requireSingleRow(result, err, requestID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET node_name_snapshot = COALESCE(
		(SELECT name FROM nodes WHERE nodes.id = agent_enrollment_requests.reserved_node_id),
		node_name_snapshot) WHERE id = ?`, requestID); err != nil {
		return fmt.Errorf("release enrollment reservation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM nodes WHERE credential_state = ? AND agent_token_hash IS NULL
		AND id = (SELECT reserved_node_id FROM agent_enrollment_requests WHERE id = ?)`,
		string(agentenrollment.CredentialPendingClaim), requestID); err != nil {
		return fmt.Errorf("release enrollment reservation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET reserved_node_id = NULL WHERE id = ?`,
		requestID); err != nil {
		return fmt.Errorf("release enrollment reservation: %w", err)
	}
	return nil
}

func requireSingleRow(result sql.Result, err error, requestID string) error {
	if err != nil {
		return fmt.Errorf("decide enrollment request: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("decide enrollment request: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("decide enrollment request %s: %w", requestID, ErrEnrollmentRequestUnavailable)
	}
	return nil
}

// autoApproveEnrollmentRequest 按授权策略自动审批；策略冲突时保持 pending 并记录原因码。
func autoApproveEnrollmentRequest(ctx context.Context, tx *sql.Tx, grant EnrollmentGrant, id string, now time.Time) error {
	request, err := loadEnrollmentRequestTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if request.Status != agentenrollment.RequestPending {
		return nil
	}
	decision, reason, ok := automaticEnrollmentReservation(grant, *request)
	if !ok {
		return recordEnrollmentConflict(ctx, tx, enrollmentConflict{id: id, reason: reason, now: now})
	}
	taken, err := enrollmentNodeNameTaken(ctx, tx, decision.Name)
	if err != nil {
		return err
	}
	if taken {
		return recordEnrollmentConflict(ctx, tx, enrollmentConflict{id: id, reason: enrollmentReasonNameConflict, now: now})
	}
	return reserveEnrollmentNode(ctx, tx, reservation{
		RequestID: id, GrantID: grant.ID, Name: decision.Name, GroupID: decision.GroupID, Tags: decision.Tags,
		Remarks: decision.Remarks, Public: decision.Public, Host: decision.Host, SSHPort: decision.SSHPort,
		ActorID: grant.CreatedBy, Now: now.UTC(),
	})
}

// automaticEnrollmentReservation 由固定策略与上报主机名推导保留节点；不追加随机后缀。
func automaticEnrollmentReservation(grant EnrollmentGrant, request EnrollmentRequest) (reservation, string, bool) {
	name, err := agentenrollment.DeriveNodeName(grant.NodeNamePrefix, request.Metadata.Hostname)
	if err != nil {
		return reservation{}, enrollmentReasonPolicyConflict, false
	}
	source, err := agentenrollment.CompileSourcePolicy(grant.SourceCIDRs, grant.HostnamePattern)
	if err != nil || !source.AllowsHostname(request.Metadata.Hostname) {
		return reservation{}, enrollmentReasonPolicyConflict, false
	}
	host := request.Metadata.AdvertisedHost
	if grant.SourceIPAsHost {
		host = request.SourceIP
	}
	return reservation{Name: name, GroupID: grant.DefaultGroupID, Tags: grant.Tags, Remarks: grant.Remarks,
		Public: grant.Public, Host: host, SSHPort: grant.SSHPort}, "", true
}

// enrollmentConflict 是一次自动审批冲突：保持 pending 并留下原因码与审计行。
type enrollmentConflict struct {
	id     string
	reason string
	now    time.Time
}

func recordEnrollmentConflict(ctx context.Context, tx *sql.Tx, conflict enrollmentConflict) error {
	if _, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET rejection_reason = ?
		WHERE id = ? AND status = 'pending'`, conflict.reason, conflict.id); err != nil {
		return fmt.Errorf("record enrollment conflict: %w", err)
	}
	event := enrollmentRequestAuditEvent(enrollmentRequestAuditInput{Action: EnrollmentActionRequestConflict,
		RequestID: conflict.id, Detail: map[string]any{"reason": conflict.reason}, Now: conflict.now})
	return writeEnrollmentAudit(ctx, tx, event)
}

func enrollmentNodeNameTaken(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var taken bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE name = ?)`, name).Scan(&taken); err != nil {
		return false, fmt.Errorf("check enrollment node name: %w", err)
	}
	return taken, nil
}

func validateEnrollmentNodeName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: node name is required", agentenrollment.ErrInvalidMetadata)
	}
	if len(name) > maxEnrollmentNodeNameLength {
		return fmt.Errorf("%w: node name must be at most %d bytes", agentenrollment.ErrInvalidMetadata,
			maxEnrollmentNodeNameLength)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: node name contains control characters", agentenrollment.ErrInvalidMetadata)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: node name is not valid UTF-8", agentenrollment.ErrInvalidMetadata)
	}
	return nil
}

// loadEnrollmentRequestTx 在事务内读取请求，供审批与自动审批共用。
func loadEnrollmentRequestTx(ctx context.Context, tx *sql.Tx, id string) (*EnrollmentRequest, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+enrollmentRequestColumns+enrollmentRequestFrom+` WHERE r.id = ?`, id)
	request, err := scanEnrollmentRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load enrollment request: %w", err)
	}
	return &request, nil
}

// enrollmentGrantForUpdate 在事务内读取授权，未知或不可用返回同一个不可用错误。
func enrollmentGrantForUpdate(ctx context.Context, tx *sql.Tx, id string) (*EnrollmentGrant, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+enrollmentGrantColumns+`
		FROM agent_enrollment_grants g LEFT JOIN groups gr ON gr.id = g.default_group_id WHERE g.id = ?`, id)
	grant, err := scanEnrollmentGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load enrollment grant: %w", ErrEnrollmentGrantUnavailable)
	}
	if err != nil {
		return nil, fmt.Errorf("load enrollment grant: %w", err)
	}
	return &grant, nil
}
