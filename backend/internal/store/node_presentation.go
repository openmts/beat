package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const maxNodeSortItems = 1000

var ErrInvalidNodeSort = errors.New("invalid node sort order")

func (s *NodeStore) UpdateNodeSort(ctx context.Context, ids []string) error {
	if len(ids) == 0 || len(ids) > maxNodeSortItems || hasDuplicateNodeIDs(ids) {
		return ErrInvalidNodeSort
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin node sort update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	groupID, err := validateNodeSortGroup(ctx, tx, ids)
	if err != nil {
		return err
	}
	if err := requireCompleteNodeGroup(ctx, tx, groupID, len(ids)); err != nil {
		return err
	}
	for index, id := range ids {
		result, err := tx.ExecContext(ctx,
			"UPDATE nodes SET sort_order = ?, updated_at = CURRENT_TIMESTAMP "+
				"WHERE id = ? AND COALESCE(group_id, '') = ?",
			index, id, groupID)
		if err != nil {
			return fmt.Errorf("update node sort order: %w", err)
		}
		if err := requireOneUpdatedNode(result); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit node sort update: %w", err)
	}
	return nil
}

func validateNodeSortGroup(ctx context.Context, tx *sql.Tx, ids []string) (string, error) {
	groupID := ""
	for index, id := range ids {
		var current sql.NullString
		if err := tx.QueryRowContext(ctx, "SELECT group_id FROM nodes WHERE id = ?", id).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", ErrInvalidNodeSort
			}
			return "", fmt.Errorf("query node sort group: %w", err)
		}
		value := current.String
		if index == 0 {
			groupID = value
		} else if value != groupID {
			return "", ErrInvalidNodeSort
		}
	}
	return groupID, nil
}

func requireCompleteNodeGroup(ctx context.Context, tx *sql.Tx, groupID string, provided int) error {
	var count int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM nodes WHERE COALESCE(group_id, '') = ?", groupID,
	).Scan(&count); err != nil {
		return fmt.Errorf("count node sort group: %w", err)
	}
	if count != provided {
		return ErrInvalidNodeSort
	}
	return nil
}

func hasDuplicateNodeIDs(ids []string) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return true
		}
		if _, exists := seen[id]; exists {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}

func requireOneUpdatedNode(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read node sort update count: %w", err)
	}
	if count != 1 {
		return ErrInvalidNodeSort
	}
	return nil
}
