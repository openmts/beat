package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

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
