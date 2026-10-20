package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const maxCustomMetricDefinitionsPerNode = 64

const maxCustomMetricSnapshotBytes = 256 << 10

func (store *CustomMetricStore) QueueNodeCustomMetrics(ctx context.Context, nodeID string) (*model.AgentTask, error) {
	ctx, cancel := context.WithTimeout(ctx, customMetricChangeTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return nil, fmt.Errorf("begin custom metric registration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	task, err := queueCustomMetricRegistration(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit custom metric registration: %w", err)
	}
	return task, nil
}

func queueCustomMetricRegistration(ctx context.Context, db schemaExecutor, nodeID string) (*model.AgentTask, error) {
	metrics, err := customMetricExecutionSnapshot(ctx, db, nodeID)
	if err != nil {
		return nil, err
	}
	payload, err := model.EncodeCustomMetricRegisterPayload(metrics)
	if err != nil {
		return nil, fmt.Errorf("encode custom metric snapshot: %w", err)
	}
	if len(payload) > maxCustomMetricSnapshotBytes {
		return nil, fmt.Errorf("%w: snapshot exceeds 256 KiB", ErrCustomMetricLimit)
	}
	createdAt, err := nextCustomMetricRegistrationTime(ctx, db, nodeID)
	if err != nil {
		return nil, err
	}
	task := &model.AgentTask{ID: uuid.NewString(), NodeID: nodeID, Type: model.AgentTaskCustomMetricRegister,
		Payload: payload, Status: model.AgentTaskStatusPending, CreatedAt: createdAt}
	_, err = db.ExecContext(ctx, "INSERT INTO agent_tasks(id,node_id,type,payload,status,created_at) VALUES(?,?,?,?,?,?)",
		task.ID, task.NodeID, task.Type, task.Payload, task.Status, task.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("persist custom metric registration: %w", err)
	}
	if err := supersedeCustomMetricTasks(ctx, db, *task); err != nil {
		return nil, err
	}
	return task, nil
}

func customMetricExecutionSnapshot(ctx context.Context, db schemaExecutor, nodeID string) ([]model.CustomMetric, error) {
	rows, err := db.QueryContext(ctx, `SELECT m.id,m.name,m.shell_command,m.interval_seconds,d.metric_key,m.updated_at FROM custom_metrics m
		JOIN custom_metric_nodes n ON n.metric_id=m.id LEFT JOIN metric_definitions d ON d.metric_key='custom_'||m.name
		WHERE n.node_id=? AND m.enabled=1 AND COALESCE(d.enabled,1)=1 ORDER BY m.name LIMIT ?`, nodeID, maxCustomMetricDefinitionsPerNode+1)
	if err != nil {
		return nil, fmt.Errorf("read custom metric snapshot: %w", err)
	}
	defer func() { _ = rows.Close() }()
	metrics := make([]model.CustomMetric, 0)
	for rows.Next() {
		var metric model.CustomMetric
		var registered sql.NullString
		if err := rows.Scan(&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds, &registered, &metric.UpdatedAt); err != nil {
			return nil, err
		}
		metric.Enabled = true
		if err := validateCustomMetric(metric); err != nil {
			return nil, err
		}
		if !registered.Valid {
			return nil, fmt.Errorf("%w: custom metric is not registered", ErrCustomMetricInvalid)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(metrics) > maxCustomMetricDefinitionsPerNode {
		return nil, fmt.Errorf("%w: a node supports at most 64 enabled custom metrics", ErrCustomMetricLimit)
	}
	return metrics, nil
}

func nextCustomMetricRegistrationTime(ctx context.Context, db schemaExecutor, nodeID string) (time.Time, error) {
	var previous time.Time
	err := db.QueryRowContext(ctx, "SELECT created_at FROM agent_tasks WHERE node_id=? AND type=? ORDER BY created_at DESC LIMIT 1", nodeID, model.AgentTaskCustomMetricRegister).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("read custom metric snapshot revision: %w", err)
	}
	now := model.NowUTC()
	if !now.After(previous) {
		now = previous.Add(time.Nanosecond)
	}
	return now, nil
}
