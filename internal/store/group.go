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
	mutation, err := beginMetadataMutation(ctx, tx, false)
	if err != nil {
		return nil, err
	}
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
	if err := mutation.finish(metadataEvidence{Action: "group.create", Type: "group", ID: g.ID, Name: g.Name, Fields: []string{"name"}}); err != nil {
		return nil, err
	}
	if err := verifyGroupSnapshot(ctx, tx, *g); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create group: %w", err)
	}
	return g, nil
}

func (s *GroupStore) UpdateGroup(ctx context.Context, id string, name string) error {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, false)
	if err != nil {
		return err
	}
	now := model.NowUTC()
	result, err := tx.ExecContext(ctx,
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
	if err := mutation.finish(metadataEvidence{Action: "group.update", Type: "group", ID: id, Name: name, Fields: []string{"name"}}); err != nil {
		return err
	}
	if err := verifyGroupChange(ctx, tx, metadataEvidence{ID: id, Name: name, Action: "group.update"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *GroupStore) DeleteGroup(ctx context.Context, id string) error {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, false)
	if err != nil {
		return err
	}
	var deletedName string
	if err := tx.QueryRowContext(ctx, "SELECT name FROM groups WHERE id=?", id).Scan(&deletedName); err != nil {
		return err
	}
	if err := deleteGroupData(ctx, tx, id); err != nil {
		return err
	}

	if err := mutation.finish(metadataEvidence{Action: "group.delete", Type: "group", ID: id, Name: deletedName}); err != nil {
		return err
	}
	if err := verifyGroupChange(ctx, tx, metadataEvidence{ID: id, Action: "group.delete"}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func (s *GroupStore) SetDefaultGroup(ctx context.Context, id string) error {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, false)
	if err != nil {
		return err
	}
	var groupName string
	if err := tx.QueryRowContext(ctx, "SELECT name FROM groups WHERE id=?", id).Scan(&groupName); err != nil {
		return err
	}
	if err := setDefaultGroupData(ctx, tx, id); err != nil {
		return err
	}

	if err := mutation.finish(metadataEvidence{Action: "group.default", Type: "group", ID: id, Name: groupName, Fields: []string{"is_default"}}); err != nil {
		return err
	}
	if err := verifyGroupChange(ctx, tx, metadataEvidence{ID: id, Action: "group.default"}); err != nil {
		return err
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
	mutation, err := beginMetadataMutation(ctx, tx, false)
	if err != nil {
		return err
	}

	if err := sortGroupData(ctx, tx, ids); err != nil {
		return err
	}

	if err := mutation.finish(metadataEvidence{Action: "group.sort", Type: "group_order", ID: "all", Fields: []string{"sort_order"}, Count: len(ids)}); err != nil {
		return err
	}
	if err := verifyMetadataOrder(ctx, tx, metadataOrder{table: "groups", ids: ids}); err != nil {
		return err
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
