package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentenrollment"
)

// 入网请求存储错误：未知与不可用分开暴露，容量与冲突用于管理端给出可纠正的原因。
var (
	ErrEnrollmentRequestNotFound    = errors.New("enrollment request not found")
	ErrEnrollmentRequestUnavailable = errors.New("enrollment request is not available")
	ErrEnrollmentRequestCapacity    = errors.New("enrollment request capacity is exhausted")
)

// 入网请求的容量与生命周期边界：请求在最后一次成功创建或恢复后最多存活三天。
const (
	maxOpenRequestsPerGrant      = 1024
	enrollmentRequestLifetime    = 72 * time.Hour
	maxEnrollmentRequestPageSize = 200
	defaultEnrollmentRequestPage = 50
)

// EnrollmentRequest 是等待审批或已认领的入网请求。
type EnrollmentRequest struct {
	ID, GrantID, GrantName       string
	KeyFingerprint, PublicKey    []byte
	TokenHash                    []byte
	TokenPrefix                  string
	TicketHash                   []byte
	TokenRotatedAt               time.Time
	Status                       agentenrollment.RequestStatus
	Metadata                     agentenrollment.Metadata
	SourceIP                     string
	FirstRequestAt, LastRequest  time.Time
	ReservedNodeID, ReservedName sql.NullString
	NodeNameSnapshot             string
	ApprovedBy                   string
	ApprovedAt                   sql.NullTime
	RejectedBy                   string
	RejectedAt                   sql.NullTime
	RejectionReason              string
	ClaimedAt                    sql.NullTime
	TicketGeneration             int
	TicketExpiresAt              sql.NullTime
	RecoveryDeadline             sql.NullTime
	RecoveryAttempts             int
	RequestExpiresAt             time.Time
}

// EnrollmentRequestInput 是创建或恢复请求所需的输入。
type EnrollmentRequestInput struct {
	GrantID     string
	Fingerprint []byte
	PublicKey   []byte
	Token       agentenrollment.Secret
	Metadata    agentenrollment.Metadata
	SourceIP    string
	Now         time.Time
}

const enrollmentRequestColumns = `r.id, r.grant_id, COALESCE(g.name, ''), r.key_fingerprint, r.public_key,
	r.token_hash, r.token_prefix, r.ticket_hash, r.token_rotated_at, r.status, r.hostname, r.advertised_host, r.os,
	r.architecture, r.agent_version, r.virtualization, r.ssh_port, r.installation_nonce, r.source_ip,
	r.first_request_at, r.last_request_at, r.reserved_node_id, COALESCE(n.name, ''), r.node_name_snapshot,
	r.approved_by, r.approved_at, r.rejected_by, r.rejected_at, r.rejection_reason, r.claimed_at,
	r.ticket_generation, r.ticket_expires_at, r.recovery_deadline, r.recovery_attempts, r.request_expires_at`

const enrollmentRequestFrom = ` FROM agent_enrollment_requests r
	LEFT JOIN agent_enrollment_grants g ON g.id = r.grant_id
	LEFT JOIN nodes n ON n.id = r.reserved_node_id`

// CreateOrResumeRequest 创建或复用未认领请求；自动授权在同一事务里完成审批。
func (s *AgentEnrollmentStore) CreateOrResumeRequest(ctx context.Context, input EnrollmentRequestInput) (EnrollmentRequest, bool, error) {
	if err := input.Metadata.Validate(); err != nil {
		return EnrollmentRequest{}, false, err
	}
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return EnrollmentRequest{}, false, fmt.Errorf("create enrollment request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	grant, err := enrollmentGrantForUpdate(ctx, tx, input.GrantID)
	if err != nil {
		return EnrollmentRequest{}, false, err
	}
	if !grant.Available(input.Now.UTC()) {
		return EnrollmentRequest{}, false, fmt.Errorf("create enrollment request: %w", ErrEnrollmentGrantUnavailable)
	}
	resumed, id, err := upsertEnrollmentRequest(ctx, tx, *grant, input)
	if err != nil {
		return EnrollmentRequest{}, false, err
	}
	if grant.Mode == agentenrollment.ApprovalAutomatic {
		if err := autoApproveEnrollmentRequest(ctx, tx, *grant, id, input.Now.UTC()); err != nil {
			return EnrollmentRequest{}, false, err
		}
	}
	if err := writeEnrollmentAudit(ctx, tx, requestCreateAudit(ctx, input, id, resumed)); err != nil {
		return EnrollmentRequest{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentRequest{}, false, fmt.Errorf("create enrollment request: %w", err)
	}
	request, err := s.EnrollmentRequest(ctx, id)
	if err != nil {
		return EnrollmentRequest{}, false, err
	}
	return *request, resumed, nil
}

// requestCreateAudit 区分新建与恢复；明细只保留可读的环境字段，不含密钥与令牌。
func requestCreateAudit(ctx context.Context, input EnrollmentRequestInput, id string, resumed bool) enrollmentAuditEvent {
	action := EnrollmentActionRequestCreate
	if resumed {
		action = EnrollmentActionRequestResume
	}
	return enrollmentRequestAuditEvent(enrollmentRequestAuditInput{Action: action, RequestID: id, Audit: enrollmentAuditFrom(ctx),
		Now: input.Now, Detail: map[string]any{
			"grant_id": input.GrantID, "hostname": input.Metadata.Hostname, "os": input.Metadata.OS,
			"architecture": input.Metadata.Architecture, "agent_version": input.Metadata.AgentVersion,
		}})
}

// upsertEnrollmentRequest 在事务内复用同授权同密钥的未认领请求，否则新建一条。
func upsertEnrollmentRequest(ctx context.Context, tx *sql.Tx, grant EnrollmentGrant, input EnrollmentRequestInput) (bool, string, error) {
	existing, err := openEnrollmentRequest(ctx, tx, input.GrantID, input.Fingerprint)
	if err != nil {
		return false, "", err
	}
	if existing != nil {
		if !bytes.Equal(existing.PublicKey, input.PublicKey) {
			return false, "", fmt.Errorf("resume enrollment request: %w", ErrEnrollmentRequestUnavailable)
		}
		if err := rotateEnrollmentToken(ctx, tx, existing.ID, input); err != nil {
			return false, "", err
		}
		return true, existing.ID, nil
	}
	open, err := countOpenEnrollmentRequests(ctx, tx, input.GrantID)
	if err != nil {
		return false, "", err
	}
	if open >= maxOpenRequestsPerGrant {
		return false, "", fmt.Errorf("create enrollment request: %w", ErrEnrollmentRequestCapacity)
	}
	id := uuid.NewString()
	if err := insertEnrollmentRequest(ctx, tx, id, input); err != nil {
		return false, "", err
	}
	return false, id, nil
}

func insertEnrollmentRequest(ctx context.Context, tx *sql.Tx, id string, input EnrollmentRequestInput) error {
	metadata, now := input.Metadata, input.Now.UTC()
	_, err := tx.ExecContext(ctx, `INSERT INTO agent_enrollment_requests (id, grant_id, key_fingerprint,
		public_key, token_hash, token_prefix, token_rotated_at, status, hostname, advertised_host, os,
		architecture, agent_version, virtualization, ssh_port, installation_nonce, source_ip,
		first_request_at, last_request_at, request_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.GrantID, input.Fingerprint, input.PublicKey, input.Token.Hash, input.Token.Prefix, now,
		metadata.Hostname, metadata.AdvertisedHost, metadata.OS, metadata.Architecture, metadata.AgentVersion,
		metadata.Virtualization, metadata.SSHPort, metadata.InstallationNonce, input.SourceIP, now, now,
		now.Add(enrollmentRequestLifetime))
	if err != nil {
		return fmt.Errorf("create enrollment request: %w", err)
	}
	return nil
}

// rotateEnrollmentToken 复用既有请求时轮换令牌并刷新元数据与存活窗口。
func rotateEnrollmentToken(ctx context.Context, tx *sql.Tx, id string, input EnrollmentRequestInput) error {
	metadata, now := input.Metadata, input.Now.UTC()
	result, err := tx.ExecContext(ctx, `UPDATE agent_enrollment_requests SET token_hash = ?, token_prefix = ?,
		token_rotated_at = ?, last_request_at = ?, request_expires_at = ?, hostname = ?, advertised_host = ?,
		os = ?, architecture = ?, agent_version = ?, virtualization = ?, ssh_port = ?, installation_nonce = ?,
		source_ip = ? WHERE id = ? AND status IN ('pending', 'approved')`,
		input.Token.Hash, input.Token.Prefix, now, now, now.Add(enrollmentRequestLifetime), metadata.Hostname,
		metadata.AdvertisedHost, metadata.OS, metadata.Architecture, metadata.AgentVersion, metadata.Virtualization,
		metadata.SSHPort, metadata.InstallationNonce, input.SourceIP, id)
	if err != nil {
		return fmt.Errorf("resume enrollment request: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("resume enrollment request: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("resume enrollment request: %w", ErrEnrollmentRequestUnavailable)
	}
	return nil
}

func openEnrollmentRequest(ctx context.Context, tx *sql.Tx, grantID string, fingerprint []byte) (*EnrollmentRequest, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+enrollmentRequestColumns+enrollmentRequestFrom+`
		WHERE r.grant_id = ? AND r.key_fingerprint = ? AND r.status IN ('pending', 'approved')`,
		grantID, fingerprint)
	request, err := scanEnrollmentRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load open enrollment request: %w", err)
	}
	return &request, nil
}

func countOpenEnrollmentRequests(ctx context.Context, tx *sql.Tx, grantID string) (int, error) {
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_enrollment_requests
		WHERE grant_id = ? AND status IN ('pending', 'approved')`, grantID).Scan(&open); err != nil {
		return 0, fmt.Errorf("count enrollment requests: %w", err)
	}
	return open, nil
}

// EnrollmentRequest 读取单条请求；未知 ID 返回 ErrEnrollmentRequestNotFound。
func (s *AgentEnrollmentStore) EnrollmentRequest(ctx context.Context, id string) (*EnrollmentRequest, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+enrollmentRequestColumns+enrollmentRequestFrom+` WHERE r.id = ?`, id)
	request, err := scanEnrollmentRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load enrollment request: %w", err)
	}
	return &request, nil
}

type enrollmentRequestScanner interface{ Scan(dest ...any) error }

func scanEnrollmentRequest(source enrollmentRequestScanner) (EnrollmentRequest, error) {
	var request EnrollmentRequest
	var status string
	err := source.Scan(&request.ID, &request.GrantID, &request.GrantName, &request.KeyFingerprint,
		&request.PublicKey, &request.TokenHash, &request.TokenPrefix, &request.TicketHash, &request.TokenRotatedAt, &status,
		&request.Metadata.Hostname, &request.Metadata.AdvertisedHost, &request.Metadata.OS,
		&request.Metadata.Architecture, &request.Metadata.AgentVersion, &request.Metadata.Virtualization,
		&request.Metadata.SSHPort, &request.Metadata.InstallationNonce, &request.SourceIP, &request.FirstRequestAt,
		&request.LastRequest, &request.ReservedNodeID, &request.ReservedName, &request.NodeNameSnapshot,
		&request.ApprovedBy, &request.ApprovedAt, &request.RejectedBy, &request.RejectedAt,
		&request.RejectionReason, &request.ClaimedAt, &request.TicketGeneration, &request.TicketExpiresAt,
		&request.RecoveryDeadline, &request.RecoveryAttempts, &request.RequestExpiresAt)
	if err != nil {
		return EnrollmentRequest{}, err
	}
	request.Status = agentenrollment.RequestStatus(status)
	return request, nil
}

// EnrollmentRequestFilter 是管理端请求列表的允许过滤器。
type EnrollmentRequestFilter struct {
	Status  agentenrollment.RequestStatus
	GrantID string
	Cursor  string
	Limit   int
}

// EnrollmentRequestPage 是一页请求与下一页游标。
type EnrollmentRequestPage struct {
	Requests   []EnrollmentRequest
	NextCursor string
}

// ListEnrollmentRequests 按最后请求时间倒序返回一页请求，游标基于时间与 ID。
func (s *AgentEnrollmentStore) ListEnrollmentRequests(ctx context.Context, filter EnrollmentRequestFilter) (EnrollmentRequestPage, error) {
	query, args := enrollmentRequestListQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EnrollmentRequestPage{}, fmt.Errorf("list enrollment requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	page := EnrollmentRequestPage{Requests: make([]EnrollmentRequest, 0)}
	for rows.Next() {
		request, err := scanEnrollmentRequest(rows)
		if err != nil {
			return EnrollmentRequestPage{}, fmt.Errorf("list enrollment requests: %w", err)
		}
		page.Requests = append(page.Requests, request)
	}
	if err := rows.Err(); err != nil {
		return EnrollmentRequestPage{}, fmt.Errorf("list enrollment requests: %w", err)
	}
	if len(page.Requests) > 0 {
		last := page.Requests[len(page.Requests)-1]
		page.NextCursor = encodeEnrollmentCursor(last.LastRequest, last.ID)
	}
	return page, nil
}

// enrollmentRequestListQuery 拼接允许的过滤条件，行数与顺序都由常量决定。
func enrollmentRequestListQuery(filter EnrollmentRequestFilter) (string, []any) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 4)
	if filter.Status.Valid() {
		conditions = append(conditions, "r.status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.GrantID != "" {
		conditions = append(conditions, "r.grant_id = ?")
		args = append(args, filter.GrantID)
	}
	if at, id, ok := decodeEnrollmentCursor(filter.Cursor); ok {
		conditions = append(conditions, "(r.last_request_at < ? OR (r.last_request_at = ? AND r.id < ?))")
		args = append(args, at, at, id)
	}
	query := `SELECT ` + enrollmentRequestColumns + enrollmentRequestFrom + ` WHERE ` +
		strings.Join(conditions, " AND ") + ` ORDER BY r.last_request_at DESC, r.id DESC LIMIT ?`
	return query, append(args, enrollmentPageLimit(filter.Limit))
}

func enrollmentPageLimit(limit int) int {
	if limit <= 0 {
		return defaultEnrollmentRequestPage
	}
	if limit > maxEnrollmentRequestPageSize {
		return maxEnrollmentRequestPageSize
	}
	return limit
}

func encodeEnrollmentCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeEnrollmentCursor(cursor string) (time.Time, string, bool) {
	if cursor == "" {
		return time.Time{}, "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", false
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", false
	}
	return at.UTC(), parts[1], true
}
