package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// ErrCustomMetricProbeNotFound 覆盖「指标不存在」与「试运行任务不存在或不属于该指标」两种情况，
// 两者对管理员都是 404，且不能通过响应区分彼此。
var ErrCustomMetricProbeNotFound = errors.New("custom metric probe not found")

// QueueCustomMetricProbe 为「试运行」创建一次性任务：指标必须存在且启用，
// 目标节点必须已绑定该指标，否则不产生任何任务。
func (store *CustomMetricStore) QueueCustomMetricProbe(ctx context.Context, metricID string, nodeID string) (*model.AgentTask, error) {
	if metricID == "" || nodeID == "" {
		return nil, fmt.Errorf("%w: metric and node are required", ErrCustomMetricInvalid)
	}
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, fmt.Errorf("begin custom metric probe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	metric, err := probeMetric(ctx, tx, metricID)
	if err != nil {
		return nil, err
	}
	if err := probeNodeBinding(ctx, tx, metricID, nodeID); err != nil {
		return nil, err
	}
	payload, err := model.EncodeCustomMetricProbePayload(metric.ID, metric.ShellCommand)
	if err != nil {
		return nil, fmt.Errorf("%w: probe payload is invalid", ErrCustomMetricInvalid)
	}
	task := &model.AgentTask{ID: uuid.NewString(), NodeID: nodeID, Type: model.AgentTaskCustomMetricProbe,
		Payload: payload, Status: model.AgentTaskStatusPending, CreatedAt: model.NowUTC()}
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_tasks(id,node_id,type,payload,status,created_at) VALUES(?,?,?,?,?,?)",
		task.ID, task.NodeID, task.Type, task.Payload, task.Status, task.CreatedAt); err != nil {
		return nil, fmt.Errorf("persist custom metric probe: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit custom metric probe: %w", err)
	}
	return task, nil
}

func probeMetric(ctx context.Context, db schemaExecutor, metricID string) (model.CustomMetric, error) {
	var metric model.CustomMetric
	var enabled int
	err := db.QueryRowContext(ctx, "SELECT id,name,shell_command,interval_seconds,description,enabled,created_at,updated_at FROM custom_metrics WHERE id=?",
		metricID).Scan(&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds, &metric.Description, &enabled, &metric.CreatedAt, &metric.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CustomMetric{}, ErrCustomMetricProbeNotFound
	}
	if err != nil {
		return model.CustomMetric{}, fmt.Errorf("read custom metric for probe: %w", err)
	}
	metric.Enabled = enabled == 1
	if !metric.Enabled {
		return model.CustomMetric{}, fmt.Errorf("%w: custom metric is disabled", ErrCustomMetricInvalid)
	}
	return metric, nil
}

func probeNodeBinding(ctx context.Context, db schemaExecutor, metricID string, nodeID string) error {
	var bound int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM custom_metric_nodes WHERE metric_id=? AND node_id=?", metricID, nodeID).Scan(&bound); err != nil {
		return fmt.Errorf("read custom metric binding for probe: %w", err)
	}
	if bound == 0 {
		return fmt.Errorf("%w: node is not bound to this custom metric", ErrCustomMetricInvalid)
	}
	return nil
}

// CustomMetricProbe 是一次试运行的结果快照；任务未结束时 Status 仍是 pending。
func (store *CustomMetricStore) CustomMetricProbe(ctx context.Context, metricID string, taskID string) (*model.AgentTask, error) {
	tasks := NewAgentTaskStore(store.db)
	task, err := tasks.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.Type != model.AgentTaskCustomMetricProbe {
		return nil, ErrCustomMetricProbeNotFound
	}
	payload, err := model.DecodeCustomMetricProbePayload(task.Payload)
	if err != nil || payload.MetricID != metricID {
		return nil, ErrCustomMetricProbeNotFound
	}
	return task, nil
}
