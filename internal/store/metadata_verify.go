package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/beat/backend/internal/model"
)

func verifyGroupSnapshot(ctx context.Context, tx *sql.Tx, expected model.Group) error {
	var actual model.Group
	err := tx.QueryRowContext(ctx, "SELECT id,name,sort_order,is_default,created_at,updated_at FROM groups WHERE id=?", expected.ID).Scan(&actual.ID, &actual.Name, &actual.SortOrder, &actual.IsDefault, &actual.CreatedAt, &actual.UpdatedAt)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("group response snapshot changed before commit")
	}
	return nil
}

func verifyGroupChange(ctx context.Context, tx *sql.Tx, evidence metadataEvidence) error {
	var count int
	query := "SELECT COUNT(*) FROM groups WHERE id=? AND name=?"
	args := []any{evidence.ID, evidence.Name}
	expected := 1
	if evidence.Action == "group.delete" {
		query = "SELECT COUNT(*) FROM groups WHERE id=?"
		args = []any{evidence.ID}
		expected = 0
	}
	if evidence.Action == "group.default" {
		query = "SELECT COUNT(*) FROM groups WHERE id=? AND is_default=1"
		args = []any{evidence.ID}
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return err
	}
	if count != expected {
		return errors.New("group mutation was not persisted")
	}
	return nil
}

type metadataOrder struct {
	table string
	ids   []string
}

func verifyMetadataOrder(ctx context.Context, tx *sql.Tx, input metadataOrder) error {
	if input.table != "groups" && input.table != "nodes" {
		return errors.New("invalid order resource")
	}
	for expected, id := range input.ids {
		var actual int
		if err := tx.QueryRowContext(ctx, "SELECT sort_order FROM "+input.table+" WHERE id=?", id).Scan(&actual); err != nil {
			return err
		}
		if expected != actual {
			return errors.New("sort order was not persisted")
		}
	}
	return nil
}

func authorizeNodeUpdate(mutation *metadataMutation, previous model.Node, next NodeUpdate) error {
	mutation.recent = previous.SSHPublicKey != next.SSHPublicKey
	return mutation.authorize()
}
