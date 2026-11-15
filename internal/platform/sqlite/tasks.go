package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"beat/internal/domain"

	"github.com/google/uuid"
)

var ErrTaskNotFound = errors.New("task definition not found")

func (s *Store) ListTaskDefinitions(ctx context.Context) ([]domain.TaskDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.slug, d.name, d.description, d.source, d.script,
		       d.interval_seconds, d.timeout_seconds, d.enabled, d.created_at, d.updated_at,
		       (SELECT MAX(COALESCE(r.completed_at, r.created_at)) FROM task_runs r WHERE r.task_id = d.id) AS last_run_at,
		       COALESCE((SELECT r.status FROM task_runs r WHERE r.task_id = d.id ORDER BY r.created_at DESC LIMIT 1), '') AS last_status,
		       (SELECT COUNT(*) FROM task_runs r WHERE r.task_id = d.id) AS run_count,
		       (SELECT COUNT(*) FROM task_runs r WHERE r.task_id = d.id AND r.status = 'success') AS success_count,
		       (SELECT COUNT(*) FROM task_runs r WHERE r.task_id = d.id AND r.status IN ('failed', 'timeout')) AS failure_count,
		       COALESCE(c.kind, 'shell_script'), COALESCE(c.config_json, '{}')
		FROM task_definitions d
		LEFT JOIN task_template_configs c ON c.task_id = d.id
		ORDER BY d.name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]domain.TaskDefinition, 0)
	for rows.Next() {
		task, err := scanTaskDefinition(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) GetTaskDefinition(ctx context.Context, id string) (domain.TaskDefinition, error) {
	tasks, err := s.ListTaskDefinitions(ctx)
	if err != nil {
		return domain.TaskDefinition{}, err
	}
	for _, task := range tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return domain.TaskDefinition{}, ErrTaskNotFound
}

func (s *Store) CreateTask(ctx context.Context, kind, name, description, script string, config json.RawMessage, intervalSeconds, timeoutSeconds int, enabled bool, now time.Time) (domain.TaskDefinition, error) {
	id := uuid.NewString()
	slug := "custom-" + id
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskDefinition{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_definitions (
			id, slug, name, description, source, script, interval_seconds,
			timeout_seconds, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'custom', ?, ?, ?, ?, ?, ?)`,
		id, slug, name, description, script, intervalSeconds, timeoutSeconds, enabled, now.Unix(), now.Unix()); err != nil {
		return domain.TaskDefinition{}, fmt.Errorf("create task: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_template_configs (task_id, kind, config_json, updated_at)
		VALUES (?, ?, ?, ?)`, id, kind, string(config), now.Unix()); err != nil {
		return domain.TaskDefinition{}, fmt.Errorf("create task config: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskDefinition{}, err
	}
	return s.GetTaskDefinition(ctx, id)
}

func (s *Store) UpdateTaskDefinition(ctx context.Context, task domain.TaskDefinition, now time.Time) (domain.TaskDefinition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskDefinition{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE task_definitions
		SET name = ?, description = ?, script = ?, interval_seconds = ?,
		    timeout_seconds = ?, enabled = ?, updated_at = ?
		WHERE id = ?`,
		task.Name, task.Description, task.Script, task.IntervalSeconds,
		task.TimeoutSeconds, task.Enabled, now.Unix(), task.ID)
	if err != nil {
		return domain.TaskDefinition{}, fmt.Errorf("update task definition: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.TaskDefinition{}, err
	}
	if affected == 0 {
		return domain.TaskDefinition{}, ErrTaskNotFound
	}
	config := task.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_template_configs (task_id, kind, config_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			kind = excluded.kind,
			config_json = excluded.config_json,
			updated_at = excluded.updated_at`,
		task.ID, task.Kind, string(config), now.Unix()); err != nil {
		return domain.TaskDefinition{}, fmt.Errorf("update task config: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskDefinition{}, err
	}
	return s.GetTaskDefinition(ctx, task.ID)
}

func (s *Store) DeleteCustomTask(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM task_definitions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete custom task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (s *Store) ScheduleAgentTaskRuns(ctx context.Context, agentID, nodeID, nodeName string, now time.Time) ([]domain.AgentTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.name, d.source, d.script, d.interval_seconds, d.timeout_seconds,
		       COALESCE(c.kind, 'shell_script'), COALESCE(c.config_json, '{}')
		FROM task_definitions d
		LEFT JOIN task_template_configs c ON c.task_id = d.id
		WHERE d.enabled = 1
		ORDER BY d.name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	type definition struct {
		id, name, source, script, kind, config string
		interval, timeout                      int
	}
	definitions := make([]definition, 0)
	for rows.Next() {
		var item definition
		if err := rows.Scan(&item.id, &item.name, &item.source, &item.script, &item.interval, &item.timeout, &item.kind, &item.config); err != nil {
			_ = rows.Close()
			return nil, err
		}
		definitions = append(definitions, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	delivered := make([]domain.AgentTask, 0)
	for _, item := range definitions {
		interval := item.interval
		if interval < 30 {
			interval = 30
		}
		scheduledFor := now.Unix() - (now.Unix() % int64(interval))
		runID := uuid.NewString()
		payload := item.script
		if item.kind != "shell_script" {
			payload = item.config
		}
		result, err := s.db.ExecContext(ctx, `
			INSERT INTO task_runs (
				id, task_id, task_name, task_source, agent_id, node_id, node_name,
				script, timeout_seconds, status, scheduled_for, scheduled_at,
				delivered_at, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'delivered', ?, ?, ?, ?)
			ON CONFLICT(task_id, agent_id, scheduled_for) DO NOTHING`,
			runID, item.id, item.name, item.source, agentID, nodeID, nodeName,
			payload, item.timeout, scheduledFor, now.Unix(), now.Unix(), now.Unix())
		if err != nil {
			return nil, fmt.Errorf("schedule task %s: %w", item.id, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}
		delivered = append(delivered, domain.AgentTask{
			ID:             runID,
			TaskID:         item.id,
			AgentID:        agentID,
			NodeID:         nodeID,
			Kind:           item.kind,
			Name:           item.name,
			Script:         item.script,
			Config:         json.RawMessage(item.config),
			TimeoutSeconds: item.timeout,
			Status:         "delivered",
			CreatedAt:      now.UTC(),
		})
	}
	return delivered, nil
}

func (s *Store) CompleteTaskRun(ctx context.Context, id, agentID, status, output string, exitCode int, durationMs int64, now time.Time) (bool, error) {
	if status != "success" && status != "failed" && status != "timeout" {
		return false, fmt.Errorf("invalid task run status %q", status)
	}
	errorMessage := ""
	if status != "success" {
		errorMessage = truncateText(output, 1024)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_runs
		SET status = ?, completed_at = ?, exit_code = ?, duration_ms = ?, output = ?, error = ?
		WHERE id = ? AND agent_id = ? AND status = 'delivered'`,
		status, now.Unix(), exitCode, durationMs, truncateText(output, 64<<10), errorMessage, id, agentID)
	if err != nil {
		return false, fmt.Errorf("complete task run: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

func (s *Store) ListTaskRuns(ctx context.Context, taskID string, limit int) ([]domain.TaskRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, task_name, task_source, agent_id, node_id, node_name,
		       '' AS script, timeout_seconds, status, scheduled_at, delivered_at,
		       completed_at, exit_code, duration_ms, substr(output, 1, 16384), error, created_at
		FROM task_runs
		WHERE (? = '' OR task_id = ?)
		ORDER BY created_at DESC
		LIMIT ?`, strings.TrimSpace(taskID), strings.TrimSpace(taskID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]domain.TaskRun, 0)
	for rows.Next() {
		run, err := scanTaskRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) GetTaskRun(ctx context.Context, id string) (domain.TaskRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, task_name, task_source, agent_id, node_id, node_name,
		       script, timeout_seconds, status, scheduled_at, delivered_at,
		       completed_at, exit_code, duration_ms, output, error, created_at
		FROM task_runs WHERE id = ?`, id)
	run, err := scanTaskRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskRun{}, ErrTaskNotFound
	}
	return run, err
}

type taskDefinitionScanner interface {
	Scan(dest ...any) error
}

func scanTaskDefinition(row taskDefinitionScanner) (domain.TaskDefinition, error) {
	var task domain.TaskDefinition
	var enabled int
	var createdAt, updatedAt int64
	var lastRunAt sql.NullInt64
	var configJSON string
	if err := row.Scan(
		&task.ID, &task.Slug, &task.Name, &task.Description, &task.Source, &task.Script,
		&task.IntervalSeconds, &task.TimeoutSeconds, &enabled, &createdAt, &updatedAt,
		&lastRunAt, &task.LastStatus, &task.RunCount, &task.SuccessCount, &task.FailureCount,
		&task.Kind, &configJSON,
	); err != nil {
		return domain.TaskDefinition{}, err
	}
	task.Enabled = enabled == 1
	task.Config = json.RawMessage(configJSON)
	task.CreatedAt = time.Unix(createdAt, 0).UTC()
	task.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if lastRunAt.Valid {
		value := time.Unix(lastRunAt.Int64, 0).UTC()
		task.LastRunAt = &value
	}
	return task, nil
}

func scanTaskRun(row taskDefinitionScanner) (domain.TaskRun, error) {
	var run domain.TaskRun
	var scheduledAt, createdAt int64
	var deliveredAt, completedAt sql.NullInt64
	if err := row.Scan(
		&run.ID, &run.TaskID, &run.TaskName, &run.TaskSource, &run.AgentID,
		&run.NodeID, &run.NodeName, &run.Script, &run.TimeoutSeconds, &run.Status,
		&scheduledAt, &deliveredAt, &completedAt, &run.ExitCode, &run.DurationMs,
		&run.Output, &run.Error, &createdAt,
	); err != nil {
		return domain.TaskRun{}, err
	}
	run.ScheduledAt = time.Unix(scheduledAt, 0).UTC()
	run.CreatedAt = time.Unix(createdAt, 0).UTC()
	if deliveredAt.Valid {
		value := time.Unix(deliveredAt.Int64, 0).UTC()
		run.DeliveredAt = &value
	}
	if completedAt.Valid {
		value := time.Unix(completedAt.Int64, 0).UTC()
		run.CompletedAt = &value
	}
	return run, nil
}

func truncateText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n[output truncated]"
}
