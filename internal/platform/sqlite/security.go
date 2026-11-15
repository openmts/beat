package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AdminSessionRecord struct {
	Username          string
	CredentialVersion int64
	CSRFToken         string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	LastSeenAt        time.Time
}

func (s *Store) CreateAdminSession(ctx context.Context, tokenHash, username string, credentialVersion int64, csrfToken string, createdAt, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO admin_sessions (
			token_hash, username, credential_version, csrf_token, created_at, expires_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tokenHash, username, credentialVersion, csrfToken, createdAt.Unix(), expiresAt.Unix(), createdAt.Unix())
	if err != nil {
		return fmt.Errorf("create admin session: %w", err)
	}
	return nil
}

func (s *Store) GetAdminSession(ctx context.Context, tokenHash string) (AdminSessionRecord, bool, error) {
	var record AdminSessionRecord
	var createdAt, expiresAt, lastSeenAt int64
	err := s.db.QueryRowContext(ctx, `
		SELECT username, credential_version, csrf_token, created_at, expires_at, last_seen_at
		FROM admin_sessions WHERE token_hash = ?`, tokenHash).
		Scan(&record.Username, &record.CredentialVersion, &record.CSRFToken, &createdAt, &expiresAt, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminSessionRecord{}, false, nil
	}
	if err != nil {
		return AdminSessionRecord{}, false, fmt.Errorf("get admin session: %w", err)
	}
	record.CreatedAt = time.Unix(createdAt, 0).UTC()
	record.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	record.LastSeenAt = time.Unix(lastSeenAt, 0).UTC()
	return record, true, nil
}

func (s *Store) TouchAdminSession(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_sessions SET last_seen_at = ? WHERE token_hash = ?`, now.Unix(), tokenHash)
	return err
}

func (s *Store) DeleteAdminSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteAllAdminSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions`)
	return err
}

type AuditEntry struct {
	Event      string
	Actor      string
	Method     string
	Path       string
	StatusCode int
	RemoteAddr string
	UserAgent  string
	Details    map[string]any
	CreatedAt  time.Time
}

func (s *Store) AppendAudit(ctx context.Context, entry AuditEntry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	details := "{}"
	if entry.Details != nil {
		encoded, err := json.Marshal(entry.Details)
		if err != nil {
			return fmt.Errorf("encode audit details: %w", err)
		}
		details = string(encoded)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (
			id, event, actor, method, path, status_code, remote_addr, user_agent, details_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), entry.Event, entry.Actor, entry.Method, entry.Path, entry.StatusCode,
		entry.RemoteAddr, truncate(entry.UserAgent, 512), details, entry.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}
	return nil
}

func (s *Store) CountAudit(ctx context.Context, event string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE (? = '' OR event = ?)`, event, event).Scan(&count)
	return count, err
}

type CleanupResult struct {
	AdminSessions    int64
	EnrollmentTokens int64
	TaskRuns         int64
	AgentTasks       int64
	AgentTombstones  int64
	AuditEntries     int64
}

func (s *Store) Cleanup(ctx context.Context, now time.Time, taskRunRetention, auditRetention time.Duration) (CleanupResult, error) {
	var result CleanupResult
	now = now.UTC()
	statements := []struct {
		target *int64
		query  string
		args   []any
	}{
		{&result.AdminSessions, `DELETE FROM admin_sessions WHERE expires_at < ?`, []any{now.Unix()}},
		{&result.EnrollmentTokens, `DELETE FROM agent_enrollment_tokens WHERE (revoked_at IS NOT NULL AND revoked_at < ?) OR (bound_agent_id IS NULL AND expires_at < ?)`, []any{now.Add(-7 * 24 * time.Hour).Unix(), now.Add(-7 * 24 * time.Hour).Unix()}},
		{&result.TaskRuns, `DELETE FROM task_runs WHERE created_at < ?`, []any{now.Add(-taskRunRetention).Unix()}},
		{&result.AgentTasks, `DELETE FROM agent_tasks WHERE created_at < ? AND status IN ('success', 'failed')`, []any{now.Add(-taskRunRetention).Unix()}},
		{&result.AgentTombstones, `DELETE FROM agent_tombstones WHERE created_at < ?`, []any{now.Add(-taskRunRetention).Unix()}},
		{&result.AuditEntries, `DELETE FROM audit_log WHERE created_at < ?`, []any{now.Add(-auditRetention).Unix()}},
	}
	for _, statement := range statements {
		outcome, err := s.db.ExecContext(ctx, statement.query, statement.args...)
		if err != nil {
			return result, fmt.Errorf("cleanup database: %w", err)
		}
		if affected, err := outcome.RowsAffected(); err == nil {
			*statement.target = affected
		}
	}
	return result, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
