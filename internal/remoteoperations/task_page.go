package remoteoperations

import (
	"context"
	"encoding/base64"
	"encoding/json"
)

type TaskPageQuery struct {
	Cursor string
	Limit  int
}

type TaskPage struct {
	Items      []TaskSummary `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type taskCursor struct {
	CreatedAt int64  `json:"created_at"`
	ID        string `json:"id"`
}

const taskSummarySelect = `SELECT t.id,t.kind,t.actor_id,t.created_at,t.expires_at,t.cancel_requested,
 COUNT(n.id),COALESCE(SUM(n.state='succeeded'),0),COALESCE(SUM(n.state IN ('failed','timed_out','output_limit','interrupted','expired')),0),
 COALESCE(SUM(n.state='queued'),0),COALESCE(SUM(n.state IN ('leased','running','unknown','cancel_requested')),0),
 CASE WHEN SUM(n.state='unknown')>0 THEN 'unknown' WHEN SUM(n.state IN ('running','leased','cancel_requested'))>0 THEN 'running'
 WHEN SUM(n.state='queued')>0 THEN 'queued' WHEN SUM(n.state IN ('failed','timed_out','output_limit','interrupted','expired'))>0 THEN 'failed'
 WHEN SUM(n.state='canceled')>0 THEN 'canceled' ELSE 'succeeded' END,COALESCE(json_extract(t.policy_snapshot,'$.restore_risk'),0)
 FROM operation_tasks t LEFT JOIN operation_task_targets n ON n.task_id=t.id`

func (service *Service) ListTaskPage(ctx context.Context, actor Actor, input TaskPageQuery) (TaskPage, error) {
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 200 {
		return TaskPage{}, ErrInvalid
	}
	cursor, err := decodeTaskCursor(input.Cursor)
	if err != nil {
		return TaskPage{}, err
	}
	var page TaskPage
	err = service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		var err error
		page, err = tx.taskPage(cursor, input.Limit)
		return err
	})
	return page, err
}

func decodeTaskCursor(encoded string) (taskCursor, error) {
	if encoded == "" {
		return taskCursor{}, nil
	}
	if len(encoded) > 256 {
		return taskCursor{}, ErrInvalid
	}
	plain, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return taskCursor{}, ErrInvalid
	}
	var cursor taskCursor
	if json.Unmarshal(plain, &cursor) != nil || cursor.CreatedAt < 1 || !validID(cursor.ID) {
		return cursor, ErrInvalid
	}
	return cursor, nil
}

func (tx transaction) taskPage(cursor taskCursor, limit int) (page TaskPage, resultErr error) {
	rows, err := tx.tx.QueryContext(tx.ctx, taskSummarySelect+` WHERE (?=0 OR t.created_at<? OR (t.created_at=? AND t.id>?))
 GROUP BY t.id ORDER BY t.created_at DESC,t.id LIMIT ?`, cursor.CreatedAt, cursor.CreatedAt, cursor.CreatedAt, cursor.ID, limit+1)
	if err != nil {
		return page, ErrUnavailable
	}
	defer func() {
		closeErr := rows.Close()
		if resultErr == nil {
			resultErr = closeErr
		}
	}()
	page.Items = []TaskSummary{}
	for rows.Next() {
		task, err := scanTaskSummary(rows)
		if err != nil {
			return page, ErrUnavailable
		}
		page.Items = append(page.Items, task)
	}
	if err := rows.Err(); err != nil {
		return page, ErrUnavailable
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[limit-1]
		encoded, err := json.Marshal(taskCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return page, ErrUnavailable
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return page, nil
}

func scanTaskSummary(row interface{ Scan(...any) error }) (TaskSummary, error) {
	var task TaskSummary
	err := row.Scan(&task.ID, &task.Kind, &task.ActorID, &task.CreatedAt, &task.ExpiresAt, &task.CancelRequested, &task.TargetCount, &task.SucceededCount,
		&task.FailedCount, &task.PendingCount, &task.RunningCount, &task.State, &task.RestoreRisk)
	return task, err
}

func (service *Service) ExecutablePolicies(ctx context.Context, actor Actor) ([]PolicyInput, error) {
	var policies []PolicyInput
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, false); err != nil {
			return err
		}
		all, err := tx.listPolicies()
		if err != nil {
			return err
		}
		policies = []PolicyInput{}
		for _, policy := range all {
			if policy.Policy.Enabled && (len(policy.NodeIDs) > 0 || len(policy.GroupIDs) > 0) {
				policies = append(policies, policy)
			}
		}
		return nil
	})
	return policies, err
}
