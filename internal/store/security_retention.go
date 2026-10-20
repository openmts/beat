package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const securityRetentionBatch = 5000

// PurgeSecurityData 按保留期限批量清理登录安全事件、指纹与过期阈值窗口。
func (store *AdminStore) PurgeSecurityData(ctx context.Context, now time.Time, batch int) (int64, error) {
	if batch < 1 || batch > securityRetentionBatch {
		return 0, ErrSecurityThresholdInvalid
	}
	cutoff := now.UTC().UnixMilli()
	total := int64(0)
	steps := []struct {
		statement string
		cutoff    int64
		limited   bool
	}{
		{`DELETE FROM security_events WHERE rowid IN (
			SELECT rowid FROM security_events WHERE retain_until < ? ORDER BY occurred_at ASC LIMIT ?)`, cutoff, true},
		{`DELETE FROM security_login_fingerprints WHERE rowid IN (
			SELECT rowid FROM security_login_fingerprints WHERE retain_until < ? ORDER BY retain_until ASC LIMIT ?)`, cutoff, true},
		{`DELETE FROM security_login_thresholds WHERE last_seen_at < ?`, cutoff - securityThresholdExpiry.Milliseconds(), false},
	}
	for _, step := range steps {
		count, err := store.execSecurityPurge(ctx, step.statement, step.cutoff, batch, step.limited)
		if err != nil {
			return total, err
		}
		total += count
	}
	return total, nil
}

func (store *AdminStore) execSecurityPurge(
	ctx context.Context, statement string, cutoff int64, batch int, limited bool,
) (int64, error) {
	args := []any{cutoff}
	if limited {
		args = append(args, batch)
	}
	result, err := store.db.ExecContext(ctx, statement, args...)
	if err != nil {
		return 0, fmt.Errorf("purge security data: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("purge security data rows: %w", err)
	}
	return count, nil
}

// SecurityEventSourceKind 是安全事件在通知消息中的来源类型。
const SecurityEventSourceKind = "security_event"

// SecurityOutboxAge 返回最旧的未投递安全通知年龄；无待投递消息时 found 为 false。
func (store *AdminStore) SecurityOutboxAge(ctx context.Context, now time.Time) (float64, bool, error) {
	var oldest sql.NullInt64
	err := store.db.QueryRowContext(ctx, `SELECT MIN(m.created_at) FROM notification_messages m
		JOIN notification_deliveries d ON d.message_id = m.id
		WHERE m.source_kind = ? AND d.state IN ('pending', 'leased')`, SecurityEventSourceKind).Scan(&oldest)
	if err != nil {
		return 0, false, fmt.Errorf("read security outbox age: %w", err)
	}
	if !oldest.Valid {
		return 0, false, nil
	}
	seconds := now.UTC().Sub(time.UnixMilli(oldest.Int64)).Seconds()
	if seconds < 0 {
		seconds = 0
	}
	return seconds, true, nil
}
