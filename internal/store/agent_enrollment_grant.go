package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/agentenrollment"
)

// 自动发现式入网授权的存储错误：未知、已撤销与已过期对外只暴露同一个不可用错误。
var (
	ErrEnrollmentGrantNotFound    = errors.New("enrollment grant not found")
	ErrEnrollmentGrantNameTaken   = errors.New("enrollment grant name is taken")
	ErrEnrollmentGrantUnavailable = errors.New("enrollment grant is not available")
	ErrEnrollmentGrantCapacity    = errors.New("enrollment grant capacity is exhausted")
	// ErrEnrollmentGrantSecretConflict 表示随机秘密与前一条授权冲突，需要重新生成。
	ErrEnrollmentGrantSecretConflict = errors.New("enrollment grant secret conflicts")
)

// EnrollmentGrant 是一次入网授权：策略固定，秘密只以哈希与前缀落库。
type EnrollmentGrant struct {
	ID, Name, SecretPrefix, DefaultGroupID, DefaultGroupName, NodeNamePrefix string
	SecretHash                                                               []byte
	Mode                                                                     agentenrollment.ApprovalMode
	Enabled                                                                  bool
	ExpiresAt, CreatedAt, UpdatedAt                                          time.Time
	RevokedAt                                                                sql.NullTime
	Tags                                                                     []string
	Remarks                                                                  string
	Public                                                                   bool
	SSHPort                                                                  int
	SourceIPAsHost                                                           bool
	SourceCIDRs                                                              []string
	HostnamePattern                                                          string
	ClaimRecovery                                                            bool
	MaximumEnrollments, ConsumedEnrollments                                  int
	CreatedBy                                                                string
}

// Policy 还原该授权的策略视图，供审批与协议路径复用同一套校验。
func (grant EnrollmentGrant) Policy() agentenrollment.GrantPolicy {
	return agentenrollment.GrantPolicy{
		Name: grant.Name, Mode: grant.Mode, MaximumEnrollments: grant.MaximumEnrollments,
		DefaultGroupID: grant.DefaultGroupID, NodeNamePrefix: grant.NodeNamePrefix, Tags: grant.Tags,
		Remarks: grant.Remarks, Public: grant.Public, SSHPort: grant.SSHPort,
		UseSourceIPAsHost: grant.SourceIPAsHost, SourceCIDRs: grant.SourceCIDRs,
		HostnamePattern: grant.HostnamePattern, ClaimRecovery: grant.ClaimRecovery,
		Enabled: grant.Enabled, ExpiresAt: grant.ExpiresAt,
	}
}

// Available 判断授权在当前时刻仍可创建请求。
func (grant EnrollmentGrant) Available(now time.Time) bool {
	return grant.Enabled && !grant.RevokedAt.Valid && grant.ExpiresAt.After(now) &&
		grant.ConsumedEnrollments < grant.MaximumEnrollments
}

// EnrollmentGrantInput 是创建授权的输入：策略、一次性秘密与操作者。
type EnrollmentGrantInput struct {
	Policy  agentenrollment.GrantPolicy
	Secret  agentenrollment.Secret
	ActorID string
}

// AgentEnrollmentStore 承载自动发现式入网的授权与请求数据。
type AgentEnrollmentStore struct{ db *sql.DB }

// NewAgentEnrollmentStore 构造授权存储。
func NewAgentEnrollmentStore(db *sql.DB) *AgentEnrollmentStore { return &AgentEnrollmentStore{db: db} }

const enrollmentGrantColumns = `g.id, g.name, g.secret_hash, g.secret_prefix, g.approval_mode, g.enabled,
	g.expires_at, g.revoked_at, g.default_group_id, COALESCE(gr.name, ''), g.node_name_prefix, g.tags_json,
	g.remarks, g.is_public, g.ssh_port, g.source_ip_as_host, g.source_cidrs_json, g.hostname_pattern,
	g.claim_recovery, g.maximum_enrollments, g.consumed_enrollments, g.created_by, g.created_at, g.updated_at`

// CreateGrant 写入一条新授权；名称冲突与全局校验失败都会在写库前拒绝。
func (s *AgentEnrollmentStore) CreateGrant(ctx context.Context, input EnrollmentGrantInput, now time.Time) (EnrollmentGrant, error) {
	policy := input.Policy
	if err := policy.Validate(now); err != nil {
		return EnrollmentGrant{}, err
	}
	id := uuid.NewString()
	record := enrollmentGrantRecord{ID: id, Policy: policy, Secret: input.Secret, ActorID: input.ActorID, Now: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnrollmentGrant{}, fmt.Errorf("create enrollment grant: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_enrollment_grants (id, name, secret_hash, secret_prefix,
		approval_mode, enabled, expires_at, revoked_at, default_group_id, node_name_prefix, tags_json, remarks,
		is_public, ssh_port, source_ip_as_host, source_cidrs_json, hostname_pattern, claim_recovery,
		maximum_enrollments, consumed_enrollments, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		id, policy.Name, input.Secret.Hash, input.Secret.Prefix, string(policy.Mode), boolToInt(policy.Enabled),
		policy.ExpiresAt.UTC(), policy.DefaultGroupID, policy.NodeNamePrefix, record.tagsJSON(),
		policy.Remarks, boolToInt(policy.Public), policy.SSHPort, boolToInt(policy.UseSourceIPAsHost),
		record.cidrJSON(), policy.HostnamePattern, boolToInt(policy.ClaimRecovery),
		policy.MaximumEnrollments, input.ActorID, now.UTC(), now.UTC()); err != nil {
		return EnrollmentGrant{}, mapEnrollmentGrantWriteError("create enrollment grant", err)
	}
	audit := enrollmentGrantAuditEvent(enrollmentGrantAuditInput{Action: EnrollmentActionGrantCreate,
		GrantID: id, Detail: enrollmentGrantPolicyDetail(policy), Audit: enrollmentAuditFrom(ctx), Now: now})
	if err := writeEnrollmentAudit(ctx, tx, audit); err != nil {
		return EnrollmentGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentGrant{}, fmt.Errorf("create enrollment grant: %w", err)
	}
	grant, err := s.Grant(ctx, id)
	if err != nil {
		return EnrollmentGrant{}, err
	}
	return *grant, nil
}

// ListGrants 返回全部授权，按创建时间倒序，供管理端列表展示。
func (s *AgentEnrollmentStore) ListGrants(ctx context.Context) ([]EnrollmentGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+enrollmentGrantColumns+`
		FROM agent_enrollment_grants g LEFT JOIN groups gr ON gr.id = g.default_group_id
		ORDER BY g.created_at DESC, g.id`)
	if err != nil {
		return nil, fmt.Errorf("list enrollment grants: %w", err)
	}
	defer func() { _ = rows.Close() }()
	grants := make([]EnrollmentGrant, 0)
	for rows.Next() {
		grant, err := scanEnrollmentGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list enrollment grants: %w", err)
	}
	return grants, nil
}

// Grant 读取单条授权，附带默认分组名称。
func (s *AgentEnrollmentStore) Grant(ctx context.Context, id string) (*EnrollmentGrant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+enrollmentGrantColumns+`
		FROM agent_enrollment_grants g LEFT JOIN groups gr ON gr.id = g.default_group_id WHERE g.id = ?`, id)
	grant, err := scanEnrollmentGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentGrantNotFound
	}
	if err != nil {
		return nil, err
	}
	return &grant, nil
}

// GrantBySecret 按哈希查找可用授权；未知、已撤销、已过期与容量耗尽返回同一个不可用错误。
func (s *AgentEnrollmentStore) GrantBySecret(ctx context.Context, plaintext string, now time.Time) (*EnrollmentGrant, error) {
	hash := agentenrollment.SecretDigest(plaintext)
	grant, err := s.grantBySecretHash(ctx, hash[:])
	if err != nil {
		return nil, err
	}
	if !grant.Available(now) || !agentenrollment.SecretMatches(grant.SecretHash, plaintext) {
		return nil, ErrEnrollmentGrantUnavailable
	}
	return grant, nil
}

func (s *AgentEnrollmentStore) grantBySecretHash(ctx context.Context, hash []byte) (*EnrollmentGrant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+enrollmentGrantColumns+`
		FROM agent_enrollment_grants g LEFT JOIN groups gr ON gr.id = g.default_group_id WHERE g.secret_hash = ?`, hash)
	grant, err := scanEnrollmentGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentGrantUnavailable
	}
	if err != nil {
		return nil, err
	}
	return &grant, nil
}

// RotateGrantSecret 轮换授权秘密，并让该授权下所有未使用的挑战立即失效。
func (s *AgentEnrollmentStore) RotateGrantSecret(ctx context.Context, id string, secret agentenrollment.Secret, now time.Time) (EnrollmentGrant, error) {
	return s.updateGrant(ctx, enrollmentGrantMutation{
		query: `UPDATE agent_enrollment_grants SET secret_hash = ?, secret_prefix = ?, updated_at = ?
			WHERE id = ? AND revoked_at IS NULL`,
		args: []any{secret.Hash, secret.Prefix, now.UTC(), id}, id: id,
		sideEffect: func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM agent_enrollment_challenges WHERE grant_id = ?`, id)
			return err
		},
		audit: &enrollmentAuditEvent{Action: EnrollmentActionGrantRotate, ResourceType: enrollmentGrantResourceType,
			ResourceID: id, Audit: enrollmentAuditFrom(ctx),
			Detail: map[string]any{"secret_prefix": secret.Prefix}, Now: now},
	})
}

// RevokeGrant 撤销授权：拒绝未认领请求、清掉挑战与票据，并删除从未持有令牌的保留节点。
func (s *AgentEnrollmentStore) RevokeGrant(ctx context.Context, id, actor string, now time.Time) (EnrollmentGrant, error) {
	return s.updateGrant(ctx, enrollmentGrantMutation{
		query: `UPDATE agent_enrollment_grants SET revoked_at = ?, enabled = 0, updated_at = ?
			WHERE id = ? AND revoked_at IS NULL`,
		args: []any{now.UTC(), now.UTC(), id}, id: id,
		sideEffect: func(tx *sql.Tx) error { return revokeEnrollmentGrantSideEffects(ctx, tx, id, actor, now) },
		audit: &enrollmentAuditEvent{Action: EnrollmentActionGrantRevoke, ResourceType: enrollmentGrantResourceType,
			ResourceID: id, Audit: enrollmentAuditFrom(ctx), Detail: map[string]any{}, Now: now},
	})
}

func revokeEnrollmentGrantSideEffects(ctx context.Context, tx *sql.Tx, grantID, actor string, now time.Time) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM agent_enrollment_challenges WHERE grant_id = ?`, []any{grantID}},
		{`UPDATE agent_enrollment_requests SET node_name_snapshot = COALESCE(
			(SELECT name FROM nodes WHERE nodes.id = reserved_node_id), node_name_snapshot)
			WHERE grant_id = ? AND reserved_node_id IS NOT NULL`, []any{grantID}},
		{`DELETE FROM nodes WHERE credential_state = 'pending_claim' AND agent_token_hash IS NULL
			AND id IN (SELECT reserved_node_id FROM agent_enrollment_requests WHERE grant_id = ?)`, []any{grantID}},
		{`UPDATE agent_enrollment_requests SET status = 'rejected', rejected_by = ?, rejected_at = ?,
			rejection_reason = 'grant_revoked', ticket_hash = NULL, ticket_expires_at = NULL,
			recovery_deadline = NULL WHERE grant_id = ? AND status IN ('pending', 'approved')`,
			[]any{actor, now.UTC(), grantID}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("revoke enrollment grant: %w", err)
		}
	}
	return nil
}

// UpdateGrant 覆盖授权策略；秘密、容量消耗与创建者保持不变。
func (s *AgentEnrollmentStore) UpdateGrant(ctx context.Context, id string, input EnrollmentGrantInput, now time.Time) (EnrollmentGrant, error) {
	if err := input.Policy.Validate(now); err != nil {
		return EnrollmentGrant{}, err
	}
	current, err := s.Grant(ctx, id)
	if err != nil {
		return EnrollmentGrant{}, err
	}
	if current.ConsumedEnrollments > input.Policy.MaximumEnrollments {
		return EnrollmentGrant{}, ErrEnrollmentGrantCapacity
	}
	record := enrollmentGrantRecord{ID: id, Policy: input.Policy, Now: now}
	audit := enrollmentGrantAuditEvent(enrollmentGrantAuditInput{Action: EnrollmentActionGrantUpdate,
		GrantID: id, Detail: enrollmentGrantPolicyDetail(input.Policy), Audit: enrollmentAuditFrom(ctx), Now: now})
	return s.updateGrant(ctx, enrollmentGrantMutation{
		query: `UPDATE agent_enrollment_grants SET name = ?, approval_mode = ?, enabled = ?,
			expires_at = ?, default_group_id = ?, node_name_prefix = ?, tags_json = ?, remarks = ?, is_public = ?,
			ssh_port = ?, source_ip_as_host = ?, source_cidrs_json = ?, hostname_pattern = ?, claim_recovery = ?,
			maximum_enrollments = ?, updated_at = ? WHERE id = ? AND revoked_at IS NULL AND consumed_enrollments <= ?`,
		args: []any{input.Policy.Name, string(input.Policy.Mode), boolToInt(input.Policy.Enabled), input.Policy.ExpiresAt.UTC(),
			input.Policy.DefaultGroupID, input.Policy.NodeNamePrefix, record.tagsJSON(), input.Policy.Remarks,
			boolToInt(input.Policy.Public), input.Policy.SSHPort, boolToInt(input.Policy.UseSourceIPAsHost),
			record.cidrJSON(), input.Policy.HostnamePattern, boolToInt(input.Policy.ClaimRecovery),
			input.Policy.MaximumEnrollments, now.UTC(), id, input.Policy.MaximumEnrollments},
		id: id, audit: &audit,
	})
}

// enrollmentGrantMutation 是一次授权写入：SQL、副作用与审计行必须在同一事务提交。
type enrollmentGrantMutation struct {
	query      string
	args       []any
	id         string
	sideEffect func(*sql.Tx) error
	audit      *enrollmentAuditEvent
}

// updateGrant 在一个立即事务里执行授权更新，并追加副作用与审计行。
func (s *AgentEnrollmentStore) updateGrant(ctx context.Context, mutation enrollmentGrantMutation) (EnrollmentGrant, error) {
	id := mutation.id
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnrollmentGrant{}, fmt.Errorf("update enrollment grant: %w", err)
	}
	result, err := tx.ExecContext(ctx, mutation.query, mutation.args...)
	if err != nil {
		_ = tx.Rollback()
		return EnrollmentGrant{}, mapEnrollmentGrantWriteError("update enrollment grant", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		_ = tx.Rollback()
		return EnrollmentGrant{}, ErrEnrollmentGrantNotFound
	}
	if mutation.sideEffect != nil {
		if err := mutation.sideEffect(tx); err != nil {
			_ = tx.Rollback()
			return EnrollmentGrant{}, err
		}
	}
	if mutation.audit != nil {
		if err := writeEnrollmentAudit(ctx, tx, *mutation.audit); err != nil {
			_ = tx.Rollback()
			return EnrollmentGrant{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return EnrollmentGrant{}, fmt.Errorf("update enrollment grant: %w", err)
	}
	grant, err := s.Grant(ctx, id)
	if err != nil {
		return EnrollmentGrant{}, err
	}
	return *grant, nil
}

type enrollmentGrantRecord struct {
	ID      string
	Policy  agentenrollment.GrantPolicy
	Secret  agentenrollment.Secret
	ActorID string
	Now     time.Time
}

func (record enrollmentGrantRecord) tagsJSON() string { return encodeGrantList(record.Policy.Tags) }

func (record enrollmentGrantRecord) cidrJSON() string {
	return encodeGrantList(record.Policy.SourceCIDRs)
}

func encodeGrantList(values []string) string {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

type grantScanner interface{ Scan(dest ...any) error }

func scanEnrollmentGrant(source grantScanner) (EnrollmentGrant, error) {
	var grant EnrollmentGrant
	var mode, tagsJSON, cidrsJSON string
	var enabled, public, sourceIPAsHost, claimRecovery int
	if err := source.Scan(&grant.ID, &grant.Name, &grant.SecretHash, &grant.SecretPrefix, &mode, &enabled,
		&grant.ExpiresAt, &grant.RevokedAt, &grant.DefaultGroupID, &grant.DefaultGroupName, &grant.NodeNamePrefix,
		&tagsJSON, &grant.Remarks, &public, &grant.SSHPort, &sourceIPAsHost, &cidrsJSON, &grant.HostnamePattern,
		&claimRecovery, &grant.MaximumEnrollments, &grant.ConsumedEnrollments, &grant.CreatedBy,
		&grant.CreatedAt, &grant.UpdatedAt); err != nil {
		return EnrollmentGrant{}, err
	}
	grant.Mode, grant.Enabled, grant.Public = agentenrollment.ApprovalMode(mode), enabled == 1, public == 1
	grant.SourceIPAsHost, grant.ClaimRecovery = sourceIPAsHost == 1, claimRecovery == 1
	grant.Tags, grant.SourceCIDRs = decodeGrantList(tagsJSON), decodeGrantList(cidrsJSON)
	return grant, nil
}

func decodeGrantList(encoded string) []string {
	values := []string{}
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return []string{}
	}
	return values
}

func mapEnrollmentGrantWriteError(action string, err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "agent_enrollment_grants.name"):
		return fmt.Errorf("%s: %w", action, ErrEnrollmentGrantNameTaken)
	case strings.Contains(message, "agent_enrollment_grants.secret_hash"):
		return fmt.Errorf("%s: %w", action, ErrEnrollmentGrantSecretConflict)
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}
