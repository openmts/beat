package remoteoperations

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/notificationqueue"
)

func (service *Service) expireQueuedTasks(ctx context.Context) error {
	ids, err := service.expiredTaskIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := service.atomicMutation(ctx, func(tx transaction) (*notificationqueue.Request, error) { return tx.expireTask(id) }); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) expiredTaskIDs(ctx context.Context) (ids []string, resultErr error) {
	rows, err := service.db.QueryContext(ctx, `SELECT id FROM operation_tasks WHERE expires_at<=? AND EXISTS(
 SELECT 1 FROM operation_task_targets n WHERE n.task_id=operation_tasks.id AND n.state IN ('queued','leased')) ORDER BY id LIMIT 64`, service.now().UnixMilli())
	if err != nil {
		return nil, ErrUnavailable
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, ErrUnavailable
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (tx transaction) expireTask(id string) (*notificationqueue.Request, error) {
	var count int
	if err := tx.tx.QueryRowContext(tx.ctx, `SELECT COUNT(*) FROM operation_task_targets n JOIN operation_tasks t ON t.id=n.task_id WHERE t.id=? AND t.expires_at<=? AND n.state IN ('queued','leased')`, id, tx.now.UnixMilli()).Scan(&count); err != nil {
		return nil, ErrUnavailable
	}
	if count == 0 {
		return nil, nil
	}
	if err := tx.exec(`UPDATE operation_attempts SET state='expired' WHERE started_at IS NULL AND target_id IN (SELECT id FROM operation_task_targets WHERE task_id=?) AND state='leased'`, id); err != nil {
		return nil, err
	}
	if err := tx.exec("UPDATE operation_task_targets SET state='expired',updated_at=? WHERE task_id=? AND state IN ('queued','leased')", tx.now.UnixMilli(), id); err != nil {
		return nil, err
	}
	if err := tx.audit("", "operation.task.queue_expired", id); err != nil {
		return nil, err
	}
	return operationFailure("task-expired:"+id, id), nil
}
