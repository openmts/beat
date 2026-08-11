package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type GroupStore struct {
	db *sql.DB
}

func NewGroupStore(db *sql.DB) *GroupStore {
	return &GroupStore{db: db}
}

var ErrDefaultGroupDelete = errors.New("store: cannot delete default group")

func (s *GroupStore) CreateGroup(ctx context.Context, name string) (*model.Group, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("begin create group: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var maxOrder sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT MAX(sort_order) FROM groups").Scan(&maxOrder)
	if err != nil {
		return nil, fmt.Errorf("querying max sort_order: %w", err)
	}

	sortOrder := 0
	if maxOrder.Valid {
		sortOrder = int(maxOrder.Int64) + 1
	}

	now := model.NowUTC()
	g := &model.Group{
		ID:        uuid.New().String(),
		Name:      name,
		SortOrder: sortOrder,
		IsDefault: false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO groups (id, name, sort_order, is_default, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		g.ID, g.Name, g.SortOrder, boolToInt(g.IsDefault), g.CreatedAt, g.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting group: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create group: %w", err)
	}
	return g, nil
}

func (s *GroupStore) ListGroups(ctx context.Context) ([]model.Group, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, sort_order, is_default, created_at, updated_at FROM groups ORDER BY sort_order ASC")
	if err != nil {
		return nil, fmt.Errorf("querying groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	groups := []model.Group{}
	for rows.Next() {
		var g model.Group
		var isDefault int
		if err := rows.Scan(&g.ID, &g.Name, &g.SortOrder, &isDefault, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning group: %w", err)
		}
		g.IsDefault = isDefault != 0
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating groups: %w", err)
	}

	return groups, nil
}

func (s *GroupStore) GetGroup(ctx context.Context, id string) (*model.Group, error) {
	var g model.Group
	var isDefault int
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, sort_order, is_default, created_at, updated_at FROM groups WHERE id = ?", id,
	).Scan(&g.ID, &g.Name, &g.SortOrder, &isDefault, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying group %s: %w", id, err)
	}
	g.IsDefault = isDefault != 0
	return &g, nil
}

func (s *GroupStore) UpdateGroup(ctx context.Context, id string, name string) error {
	now := model.NowUTC()
	result, err := s.db.ExecContext(ctx,
		"UPDATE groups SET name = ?, updated_at = ? WHERE id = ?",
		name, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating group %s: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("updating group %s rows affected: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("store: group %s not found", id)
	}
	return nil
}

func (s *GroupStore) DeleteGroup(ctx context.Context, id string) error {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
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

	_, err = tx.ExecContext(ctx,
		"UPDATE nodes SET group_id = ? WHERE group_id = ?",
		defaultID, id,
	)
	if err != nil {
		return fmt.Errorf("moving nodes to default group: %w", err)
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func (s *GroupStore) GetDefaultGroup(ctx context.Context) (*model.Group, error) {
	var g model.Group
	var isDefault int
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, sort_order, is_default, created_at, updated_at FROM groups WHERE is_default = 1 LIMIT 1",
	).Scan(&g.ID, &g.Name, &g.SortOrder, &isDefault, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying default group: %w", err)
	}
	g.IsDefault = isDefault != 0
	return &g, nil
}

func (s *GroupStore) SetDefaultGroup(ctx context.Context, id string) error {
	now := model.NowUTC()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func (s *GroupStore) UpdateSortOrder(ctx context.Context, ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return fmt.Errorf("store: duplicate group %s in sort order", id)
		}
		seen[id] = struct{}{}
	}
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
