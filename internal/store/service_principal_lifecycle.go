package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func expireByDeadline(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	result, err := tx.ExecContext(ctx, `UPDATE service_principals
		SET revoked_at = ?, revoked_reason = 'expired', revision = revision + 1
		WHERE revoked_at IS NULL AND expires_at <= ?`, now, now)
	if err != nil {
		return 0, fmt.Errorf("expire service tokens: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire service tokens: %w", err)
	}
	return int(affected), nil
}

func expireSuperseded(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, superseded_at, rotation_overlap_seconds
		FROM service_principals WHERE revoked_at IS NULL AND superseded_at IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("list superseded service tokens: %w", err)
	}
	var pending []string
	for rows.Next() {
		var id string
		var superseded time.Time
		var overlap int
		if err := rows.Scan(&id, &superseded, &overlap); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("read superseded service token: %w", err)
		}
		if !superseded.Add(time.Duration(overlap) * time.Second).After(now) {
			pending = append(pending, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterate superseded service tokens: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close superseded service tokens: %w", err)
	}
	revoked := 0
	for _, id := range pending {
		result, err := tx.ExecContext(ctx, `UPDATE service_principals
			SET revoked_at = ?, revoked_reason = 'rotated', revision = revision + 1
			WHERE id = ? AND revoked_at IS NULL`, now, id)
		if err != nil {
			return revoked, fmt.Errorf("revoke rotated service token: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return revoked, fmt.Errorf("revoke rotated service token: %w", err)
		}
		revoked += int(affected)
	}
	return revoked, nil
}
