package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
)

// ErrSecurityThresholdInvalid 表示阈值输入越界。
var ErrSecurityThresholdInvalid = errors.New("store: security threshold input is invalid")

const (
	securityThresholdMaximumRows  = 20000
	securityThresholdMaximumCount = 1000000
	securityThresholdExpiry       = 24 * time.Hour
)

// ThresholdBump 描述一个来源或用户名键在固定窗口内的失败计数。
type ThresholdBump struct {
	KeyKind string
	KeyHash []byte
	Now     time.Time
	Window  time.Duration
	Limit   int
	Lockout time.Duration
}

// ThresholdOutcome 回报计数、锁定状态与是否需要下发一次可疑事件。
type ThresholdOutcome struct {
	KeyKind    string
	KeyHash    []byte
	WindowAt   time.Time
	Count      int
	Locked     bool
	LockUntil  time.Time
	ShouldEmit bool
}

// ThresholdKey 唯一定位一条阈值窗口，用于事件下发的幂等标记。
type ThresholdKey struct {
	KeyKind  string
	KeyHash  []byte
	WindowAt time.Time
}

// LoginFailureAdmission 在同一事务内记录失败审计与持久化阈值。
type LoginFailureAdmission struct {
	Audit      model.AdminAuditEvent
	Thresholds []ThresholdBump
}

func (store *AdminStore) AdmitLoginFailure(
	ctx context.Context, admission LoginFailureAdmission,
) ([]ThresholdOutcome, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	outcomes, err := store.AdmitLoginFailureTx(ctx, tx, admission)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return outcomes, nil
}

// AdmitLoginFailureTx 供通知队列事务回调使用。
func (store *AdminStore) AdmitLoginFailureTx(
	ctx context.Context, tx *sql.Tx, admission LoginFailureAdmission,
) ([]ThresholdOutcome, error) {
	if admission.Audit.ID == "" || admission.Audit.RequestID == "" || admission.Audit.CreatedAt.IsZero() {
		return nil, ErrSecurityThresholdInvalid
	}
	if err := insertAuditEvent(ctx, tx, &admission.Audit); err != nil {
		return nil, err
	}
	outcomes := make([]ThresholdOutcome, 0, len(admission.Thresholds))
	for _, bump := range admission.Thresholds {
		outcome, err := bumpSecurityThreshold(ctx, tx, bump)
		if err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// MarkThresholdEmittedTx 在事件与通知消息已写入后标记窗口已下发；返回 false 表示重复。
func (store *AdminStore) MarkThresholdEmittedTx(
	ctx context.Context, tx *sql.Tx, key ThresholdKey, at time.Time,
) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE security_login_thresholds SET last_emitted_at=?
		WHERE key_kind=? AND key_hash=? AND window_started_at=? AND (last_emitted_at IS NULL OR last_emitted_at < ?)`,
		at.UnixMilli(), key.KeyKind, key.KeyHash, key.WindowAt.UnixMilli(), key.WindowAt.UnixMilli())
	if err != nil {
		return false, fmt.Errorf("mark security threshold emitted: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark security threshold emitted rows: %w", err)
	}
	return affected == 1, nil
}

func bumpSecurityThreshold(ctx context.Context, tx *sql.Tx, bump ThresholdBump) (ThresholdOutcome, error) {
	if err := validateThresholdBump(bump); err != nil {
		return ThresholdOutcome{}, err
	}
	outcome := ThresholdOutcome{KeyKind: bump.KeyKind, KeyHash: append([]byte(nil), bump.KeyHash...)}
	var (
		windowStart int64
		count       int64
		lastSeen    int64
		lockExpires sql.NullInt64
		lastEmitted sql.NullInt64
	)
	err := tx.QueryRowContext(ctx, `SELECT window_started_at, attempt_count, last_seen_at, lock_expires_at, last_emitted_at
		FROM security_login_thresholds WHERE key_kind=? AND key_hash=?
		ORDER BY window_started_at DESC LIMIT 1`, bump.KeyKind, bump.KeyHash).
		Scan(&windowStart, &count, &lastSeen, &lockExpires, &lastEmitted)
	if errors.Is(err, sql.ErrNoRows) || err == nil && bump.Now.Sub(time.UnixMilli(windowStart)) >= bump.Window {
		return startThresholdWindow(ctx, tx, bump)
	}
	if err != nil {
		return ThresholdOutcome{}, fmt.Errorf("read security threshold: %w", err)
	}
	outcome.WindowAt = time.UnixMilli(windowStart).UTC()
	outcome.Count = int(min(count+1, securityThresholdMaximumCount))
	if outcome.Count >= bump.Limit {
		outcome.Locked = true
		outcome.LockUntil = bump.Now.Add(bump.Lockout)
		outcome.ShouldEmit = !lastEmitted.Valid || bump.Now.Sub(time.UnixMilli(lastEmitted.Int64)) >= bump.Window
	}
	_, err = tx.ExecContext(ctx, `UPDATE security_login_thresholds SET attempt_count=?, last_seen_at=?, lock_expires_at=?
		WHERE key_kind=? AND key_hash=? AND window_started_at=?`,
		outcome.Count, bump.Now.UnixMilli(), timeMillis(lockValue(outcome, lockExpires)), bump.KeyKind, bump.KeyHash, windowStart)
	if err != nil {
		return ThresholdOutcome{}, fmt.Errorf("update security threshold: %w", err)
	}
	return outcome, nil
}

func lockValue(outcome ThresholdOutcome, existing sql.NullInt64) *time.Time {
	if outcome.Locked {
		moment := outcome.LockUntil
		return &moment
	}
	if existing.Valid {
		moment := time.UnixMilli(existing.Int64).UTC()
		return &moment
	}
	return nil
}

func startThresholdWindow(ctx context.Context, tx *sql.Tx, bump ThresholdBump) (ThresholdOutcome, error) {
	outcome := ThresholdOutcome{KeyKind: bump.KeyKind, KeyHash: append([]byte(nil), bump.KeyHash...),
		WindowAt: bump.Now.UTC(), Count: 1}
	var lockUntil *time.Time
	if bump.Limit <= 1 {
		outcome.Locked = true
		outcome.LockUntil = bump.Now.Add(bump.Lockout)
		outcome.ShouldEmit = true
		lockUntil = &outcome.LockUntil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO security_login_thresholds
		(key_kind,key_hash,window_started_at,attempt_count,lock_expires_at,last_seen_at,last_emitted_at)
		VALUES(?,?,?,?,?,?,NULL)`, bump.KeyKind, bump.KeyHash, bump.Now.UnixMilli(), 1,
		timeMillis(lockUntil), bump.Now.UnixMilli())
	if err := securityRowsAffected(result, err, "insert security threshold"); err != nil {
		return ThresholdOutcome{}, err
	}
	if err := trimSecurityThresholds(ctx, tx, bump.Now); err != nil {
		return ThresholdOutcome{}, err
	}
	return outcome, nil
}

func validateThresholdBump(bump ThresholdBump) error {
	if bump.KeyKind != SecurityFingerprintKindSource && bump.KeyKind != "username" {
		return ErrSecurityThresholdInvalid
	}
	if len(bump.KeyHash) != 32 || bump.Now.IsZero() || bump.Window <= 0 || bump.Limit < 1 || bump.Lockout <= 0 {
		return ErrSecurityThresholdInvalid
	}
	return nil
}

func trimSecurityThresholds(ctx context.Context, tx *sql.Tx, now time.Time) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM security_login_thresholds WHERE last_seen_at < ?",
		now.Add(-securityThresholdExpiry).UnixMilli()); err != nil {
		return fmt.Errorf("expire security thresholds: %w", err)
	}
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM security_login_thresholds").Scan(&total); err != nil {
		return fmt.Errorf("count security thresholds: %w", err)
	}
	excess := total - securityThresholdMaximumRows
	if excess <= 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_login_thresholds WHERE rowid IN (
		SELECT rowid FROM security_login_thresholds ORDER BY last_seen_at ASC, rowid ASC LIMIT ?)`, excess); err != nil {
		return fmt.Errorf("trim security thresholds: %w", err)
	}
	return nil
}

// EmitThresholdEvent 在没有通知队列时原子写入可疑事件并标记窗口已下发。
func (store *AdminStore) EmitThresholdEvent(
	ctx context.Context, key ThresholdKey, event model.SecurityEvent, at time.Time,
) (bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	marked, err := store.MarkThresholdEmittedTx(ctx, tx, key, at)
	if err != nil || !marked {
		return false, err
	}
	if err := insertSecurityEvents(ctx, tx, []model.SecurityEvent{event}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
