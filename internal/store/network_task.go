package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type NetworkTaskStore struct {
	db *sql.DB
}

const networkTaskColumns = `id, name, direction, type, target, ip_family, interval_seconds,
	timeout_milliseconds, packet_count, all_nodes, enabled, is_public, sort_order, created_at, updated_at,
	target_mode`

const networkTargetColumns = `id, task_id, label, carrier, region, target, ip_family,
	packet_count, visible_address, enabled, sort_order, created_at, updated_at`

func NewNetworkTaskStore(db *sql.DB) *NetworkTaskStore {
	return &NetworkTaskStore{db: db}
}

func (s *NetworkTaskStore) CreateTask(
	ctx context.Context,
	task model.NetworkTask,
	nodeIDs []string,
) (*model.NetworkTask, error) {
	task.Normalize()
	if err := validateTaskWrite(task, nodeIDs); err != nil {
		return nil, err
	}
	task.ID = uuid.New().String()
	task.CreatedAt = model.NowUTC()
	task.UpdatedAt = task.CreatedAt
	assignNewTargetIDs(&task)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin network task create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertNetworkTask(ctx, tx, task); err != nil {
		return nil, err
	}
	if err := replaceTaskTargets(ctx, tx, task); err != nil {
		return nil, err
	}
	if err := replaceTaskNodes(ctx, tx, task.ID, task.AllNodes, nodeIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit network task create: %w", err)
	}
	task.Nodes, err = s.listTaskNodes(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *NetworkTaskStore) UpdateTask(
	ctx context.Context,
	id string,
	task model.NetworkTask,
	nodeIDs []string,
) (*model.NetworkTask, error) {
	task.Normalize()
	if err := validateTaskWrite(task, nodeIDs); err != nil {
		return nil, err
	}
	existing, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Direction != task.Direction {
		return nil, errors.New("network task direction cannot be changed")
	}
	task.ID = id
	task.UpdatedAt = model.NowUTC()
	if existing != nil {
		preserveTargetIDs(existing.Targets, &task)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin network task update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE network_tasks SET name = ?, direction = ?, type = ?, target = ?,
		ip_family = ?, interval_seconds = ?, timeout_milliseconds = ?, packet_count = ?, all_nodes = ?, enabled = ?,
		is_public = ?, sort_order = ?, updated_at = ?, target_mode = ? WHERE id = ?`,
		task.Name, task.Direction, task.Type, task.Target, task.IPFamily, task.IntervalSeconds,
		task.TimeoutMilliseconds, task.PacketCount, task.AllNodes, task.Enabled, task.IsPublic,
		task.SortOrder, task.UpdatedAt, task.TargetMode, id)
	if err != nil {
		return nil, fmt.Errorf("update network task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read network task update count: %w", err)
	}
	if affected == 0 {
		return nil, nil
	}
	if err := replaceTaskTargets(ctx, tx, task); err != nil {
		return nil, err
	}
	if err := replaceTaskNodes(ctx, tx, id, task.AllNodes, nodeIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit network task update: %w", err)
	}
	return s.GetTask(ctx, id)
}

func (s *NetworkTaskStore) GetTask(ctx context.Context, id string) (*model.NetworkTask, error) {
	task, err := scanNetworkTask(s.db.QueryRowContext(ctx,
		"SELECT "+networkTaskColumns+" FROM network_tasks WHERE id = ? AND lifecycle_state = 'active'", id,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query network task: %w", err)
	}
	task.Nodes, err = s.listTaskNodes(ctx, id)
	if err != nil {
		return nil, err
	}
	task.Targets, err = s.listTaskTargets(ctx, id)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *NetworkTaskStore) ListTasks(ctx context.Context, publicOnly bool) ([]model.NetworkTask, error) {
	query := "SELECT " + networkTaskColumns + " FROM network_tasks WHERE lifecycle_state = 'active'"
	if publicOnly {
		query += " AND enabled = 1 AND is_public = 1"
	}
	query += " ORDER BY sort_order ASC, name ASC"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query network tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	tasks := []model.NetworkTask{}
	for rows.Next() {
		task, scanErr := scanNetworkTask(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan network task: %w", scanErr)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network tasks: %w", err)
	}
	for index := range tasks {
		tasks[index].Nodes, err = s.listTaskNodes(ctx, tasks[index].ID)
		if err != nil {
			return nil, err
		}
		tasks[index].Targets, err = s.listTaskTargets(ctx, tasks[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

func (s *NetworkTaskStore) DeleteTask(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM network_tasks WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("delete network task: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read network task delete count: %w", err)
	}
	return affected > 0, nil
}

func (s *NetworkTaskStore) UpdateSortOrder(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin network task sort: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for index, id := range ids {
		result, execErr := tx.ExecContext(ctx,
			"UPDATE network_tasks SET sort_order = ?, updated_at = ? WHERE id = ?",
			index, model.NowUTC(), id)
		if execErr != nil {
			return fmt.Errorf("sort network task: %w", execErr)
		}
		affected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("read network task sort count: %w", rowsErr)
		}
		if affected != 1 {
			return fmt.Errorf("sort network task: task %s not found", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit network task sort: %w", err)
	}
	return nil
}

func insertNetworkTask(ctx context.Context, tx *sql.Tx, task model.NetworkTask) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO network_tasks (`+networkTaskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, task.ID, task.Name, task.Direction,
		task.Type, task.Target, task.IPFamily, task.IntervalSeconds, task.TimeoutMilliseconds,
		task.PacketCount, task.AllNodes, task.Enabled, task.IsPublic, task.SortOrder,
		task.CreatedAt, task.UpdatedAt, task.TargetMode)
	if err != nil {
		return fmt.Errorf("insert network task: %w", err)
	}
	return nil
}

func scanNetworkTask(scanner rowScanner) (model.NetworkTask, error) {
	var task model.NetworkTask
	err := scanner.Scan(&task.ID, &task.Name, &task.Direction, &task.Type, &task.Target,
		&task.IPFamily, &task.IntervalSeconds, &task.TimeoutMilliseconds, &task.PacketCount,
		&task.AllNodes, &task.Enabled, &task.IsPublic, &task.SortOrder,
		&task.CreatedAt, &task.UpdatedAt, &task.TargetMode)
	return task, err
}

func assignNewTargetIDs(task *model.NetworkTask) {
	for index := range task.Targets {
		task.Targets[index].ID = uuid.NewString()
	}
}

func preserveTargetIDs(existing []model.NetworkTarget, task *model.NetworkTask) {
	known := make(map[string]struct{}, len(existing))
	for _, target := range existing {
		known[target.ID] = struct{}{}
	}
	for index := range task.Targets {
		if _, ok := known[task.Targets[index].ID]; ok {
			continue
		}
		task.Targets[index].ID = uuid.NewString()
	}
}

func replaceTaskTargets(ctx context.Context, tx *sql.Tx, task model.NetworkTask) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM network_task_targets WHERE task_id = ?", task.ID); err != nil {
		return fmt.Errorf("clear network task targets: %w", err)
	}
	if task.Direction != model.NetworkDirectionAgentToTarget {
		return nil
	}
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = task.UpdatedAt
	}
	for index := range task.Targets {
		target := task.Targets[index]
		if target.ID == "" {
			target.ID = uuid.NewString()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO network_task_targets (`+networkTargetColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, target.ID, task.ID, target.Label,
			target.Carrier, target.Region, target.Target, target.IPFamily, target.PacketCount,
			target.VisibleAddress, target.Enabled, target.SortOrder, createdAt, task.UpdatedAt); err != nil {
			return fmt.Errorf("insert network task target: %w", err)
		}
	}
	return nil
}

func (s *NetworkTaskStore) listTaskTargets(ctx context.Context, taskID string) ([]model.NetworkTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+networkTargetColumns+`
		FROM network_task_targets WHERE task_id = ? ORDER BY sort_order ASC, id ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query network task targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	targets := []model.NetworkTarget{}
	for rows.Next() {
		var target model.NetworkTarget
		var targetTaskID string
		var createdAt, updatedAt any
		if err := rows.Scan(&target.ID, &targetTaskID, &target.Label, &target.Carrier, &target.Region,
			&target.Target, &target.IPFamily, &target.PacketCount, &target.VisibleAddress,
			&target.Enabled, &target.SortOrder, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan network task target: %w", err)
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network task targets: %w", err)
	}
	return targets, nil
}

func validateTaskWrite(task model.NetworkTask, nodeIDs []string) error {
	if err := task.Validate(); err != nil {
		return err
	}
	if !task.AllNodes && len(nodeIDs) == 0 {
		return errors.New("explicit network task assignment requires at least one node")
	}
	seen := make(map[string]struct{}, len(nodeIDs))
	for _, id := range nodeIDs {
		if strings.TrimSpace(id) == "" {
			return errors.New("network task node ID is required")
		}
		if _, exists := seen[id]; exists {
			return errors.New("network task node IDs must be unique")
		}
		seen[id] = struct{}{}
	}
	return nil
}
