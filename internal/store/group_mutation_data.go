package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

func deleteGroupData(ctx context.Context, tx *sql.Tx, id string) error {
	var defaultID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM groups WHERE is_default = 1 LIMIT 1").Scan(&defaultID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: no default group found")
		}
		return fmt.Errorf("getting default group: %w", err)
	}
	if id == defaultID {
		return ErrDefaultGroupDelete
	}

	if err := guardOperationGroupDelete(ctx, tx, id); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx,
		"UPDATE nodes SET group_id = ? WHERE group_id = ?",
		defaultID, id,
	)
	if err != nil {
		return fmt.Errorf("moving nodes to default group: %w", err)
	}

	if err := guardAlertScopeMutation(ctx, tx); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting group %s: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleting group %s rows affected: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("store: group %s not found", id)
	}

	return nil
}

func setDefaultGroupData(ctx context.Context, tx *sql.Tx, id string) error {
	now := model.NowUTC()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE id = ?", id).Scan(&exists); err != nil {
		return fmt.Errorf("checking new default group: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("store: group %s not found", id)
	}

	result, err := tx.ExecContext(ctx, "UPDATE groups SET is_default = 0, updated_at = ? WHERE is_default = 1", now)
	if err != nil {
		return fmt.Errorf("unsetting old default: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("unsetting old default rows affected: %w", err)
	}

	result, err = tx.ExecContext(ctx,
		"UPDATE groups SET is_default = 1, updated_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return fmt.Errorf("setting new default: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("setting new default rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("store: group %s not found", id)
	}

	return nil
}

func sortGroupData(ctx context.Context, tx *sql.Tx, ids []string) error {
	for i, id := range ids {
		result, err := tx.ExecContext(ctx,
			"UPDATE groups SET sort_order = ?, updated_at = ? WHERE id = ?",
			i, model.NowUTC(), id,
		)
		if err != nil {
			return fmt.Errorf("updating sort order for group %s: %w", id, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("updating sort order for group %s rows affected: %w", id, err)
		}
		if affected == 0 {
			return fmt.Errorf("store: group %s not found", id)
		}
	}

	return nil
}
