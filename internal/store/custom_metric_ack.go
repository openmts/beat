package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
)

// AgentTaskCompletion 是 Agent 上报的任务终态；输出与失败原因都会被裁剪到 model 上限。
type AgentTaskCompletion struct{ NodeID, TaskID, Status, Output, Failure string }

type customTaskAcknowledgement struct {
	nodeID, kind, status   string
	createdAt, completedAt sql.NullTime
}

// CompleteTaskForNode 校验归属；被新快照替代的旧注册任务ACK只确认收到，不改写终态。
func (store *AgentTaskStore) CompleteTaskForNode(ctx context.Context, completion AgentTaskCompletion) error {
	if completion.NodeID == "" || (completion.Status != model.AgentTaskStatusDone && completion.Status != model.AgentTaskStatusFailed) {
		return errors.New("invalid agent task completion")
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return fmt.Errorf("begin agent task completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var task customTaskAcknowledgement
	err = tx.QueryRowContext(ctx, "SELECT node_id,type,status,created_at,completed_at FROM agent_tasks WHERE id=?", completion.TaskID).
		Scan(&task.nodeID, &task.kind, &task.status, &task.createdAt, &task.completedAt)
	if err != nil {
		return fmt.Errorf("find assigned agent task: %w", err)
	}
	if task.nodeID != completion.NodeID {
		return errors.New("agent task is not assigned to this node")
	}
	if task.status == model.AgentTaskStatusPending {
		err = completePendingAgentTask(ctx, tx, completion)
	} else {
		err = confirmSupersededCustomTask(ctx, tx, task)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit agent task completion: %w", err)
	}
	return nil
}

func completePendingAgentTask(ctx context.Context, db schemaExecutor, completion AgentTaskCompletion) error {
	now := model.NowUTC()
	result, err := db.ExecContext(ctx, "UPDATE agent_tasks SET status=?,output=?,failure=?,attempted_at=COALESCE(attempted_at,?),completed_at=? WHERE id=? AND node_id=? AND status=?",
		completion.Status, model.TruncateAgentTaskOutput(completion.Output), model.TruncateAgentTaskFailure(completion.Failure), now, now, completion.TaskID, completion.NodeID, model.AgentTaskStatusPending)
	if err != nil {
		return fmt.Errorf("complete assigned agent task: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count assigned task completion: %w", err)
	}
	if count != 1 {
		return errors.New("assigned agent task is not pending")
	}
	return nil
}

func confirmSupersededCustomTask(ctx context.Context, db schemaExecutor, task customTaskAcknowledgement) error {
	if task.kind != model.AgentTaskCustomMetricRegister || task.status != model.AgentTaskStatusFailed || !task.completedAt.Valid {
		return errors.New("agent task is not pending")
	}
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_tasks replacement JOIN admin_audit_events audit ON audit.resource_id=replacement.id
		WHERE replacement.node_id=? AND replacement.type=? AND replacement.created_at=? AND replacement.created_at>?
		AND audit.action='agent.custom_metrics.supersede' AND audit.outcome='success'`, task.nodeID, task.kind, task.completedAt.Time, task.createdAt.Time).Scan(&count)
	if err != nil {
		return fmt.Errorf("verify superseded custom metric task: %w", err)
	}
	if count == 0 {
		return errors.New("agent task is not pending")
	}
	return nil
}
