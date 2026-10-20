package store

import (
	"context"
	"errors"
	"strings"
)

var ErrOperationNodeBusy = errors.New("node has active remote operations")

var ErrOperationGroupBusy = errors.New("group is assigned to enabled operation policies")

type OperationGroupConflict struct{ Policies []string }

func (conflict OperationGroupConflict) Error() string {
	return ErrOperationGroupBusy.Error() + ": " + strings.Join(conflict.Policies, ", ")
}

func (conflict OperationGroupConflict) Unwrap() error { return ErrOperationGroupBusy }

func operationSchemaPresent(ctx context.Context, db schemaExecutor) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='operation_task_targets'").Scan(&count)
	return count != 0, err
}

func guardOperationNodeDelete(ctx context.Context, db schemaExecutor, id string) error {
	present, err := operationSchemaPresent(ctx, db)
	if err != nil || !present {
		return err
	}
	var count int
	err = db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM operation_task_targets WHERE node_id=? AND state IN ('queued','leased','running','unknown','cancel_requested'))+
 (SELECT COUNT(*) FROM operation_transfer_targets WHERE node_id=? AND state IN ('queued','leased','running','unknown','cancel_requested','uploading'))`, id, id).Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return ErrOperationNodeBusy
	}
	return nil
}

func guardOperationGroupDelete(ctx context.Context, db schemaExecutor, id string) (resultErr error) {
	present, err := operationSchemaPresent(ctx, db)
	if err != nil || !present {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT p.name FROM operation_policies p JOIN operation_policy_groups g ON g.policy_id=p.id WHERE g.group_id=? AND p.enabled=1 ORDER BY p.id LIMIT 200`, id)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(names) != 0 {
		return OperationGroupConflict{Policies: names}
	}
	return nil
}
