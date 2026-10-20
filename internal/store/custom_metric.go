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

const customMetricColumns = `id, name, shell_command, interval_seconds, description, enabled, created_at, updated_at`

var ErrCustomMetricNameTaken = errors.New("custom metric name already exists")

type CustomMetricStore struct {
	db *sql.DB
}

func NewCustomMetricStore(db *sql.DB) *CustomMetricStore {
	return &CustomMetricStore{db: db}
}

func (s *CustomMetricStore) Create(ctx context.Context, metric model.CustomMetric) (*model.CustomMetric, error) {
	if err := validateCustomMetric(metric); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, customMetricChangeTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	now := model.NowUTC()
	metric.ID = uuid.New().String()
	metric.CreatedAt = now
	metric.UpdatedAt = now
	_, err = tx.ExecContext(ctx,
		"INSERT INTO custom_metrics (id, name, shell_command, interval_seconds, description, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		metric.ID, metric.Name, metric.ShellCommand, metric.IntervalSeconds,
		metric.Description, boolInt(metric.Enabled), metric.CreatedAt, metric.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrCustomMetricNameTaken
		}
		return nil, fmt.Errorf("inserting custom metric: %w", err)
	}
	definition, err := customCatalogDefinition(metric.Name, metric.Description)
	if err != nil {
		return nil, err
	}
	if err := ensureMetricDefinition(ctx, tx, definition); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &metric, nil
}

func (s *CustomMetricStore) Update(ctx context.Context, id string, metric model.CustomMetric) (*model.CustomMetric, error) {
	change, err := s.UpdateWithRegistration(ctx, id, metric)
	return change.Metric, err
}

func (s *CustomMetricStore) Delete(ctx context.Context, id string) error {
	_, err := s.DeleteWithRegistration(ctx, id)
	return err
}

func (s *CustomMetricStore) Get(ctx context.Context, id string) (*model.CustomMetric, error) {
	var metric model.CustomMetric
	err := s.db.QueryRowContext(ctx,
		"SELECT "+customMetricColumns+" FROM custom_metrics WHERE id = ?", id,
	).Scan(
		&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds,
		&metric.Description, &metric.Enabled, &metric.CreatedAt, &metric.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying custom metric: %w", err)
	}
	return &metric, nil
}

func (s *CustomMetricStore) List(ctx context.Context) ([]model.CustomMetric, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+customMetricColumns+" FROM custom_metrics ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("querying custom metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []model.CustomMetric
	for rows.Next() {
		var metric model.CustomMetric
		if err := rows.Scan(
			&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds,
			&metric.Description, &metric.Enabled, &metric.CreatedAt, &metric.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning custom metric: %w", err)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating custom metrics: %w", err)
	}
	return metrics, nil
}

// ListEnabledForNode returns the enabled custom metrics bound to a node.
func (s *CustomMetricStore) ListEnabledForNode(ctx context.Context, nodeID string) ([]model.CustomMetric, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.name, m.shell_command, m.interval_seconds,
			m.description, m.enabled, m.created_at, m.updated_at
		FROM custom_metrics m JOIN custom_metric_nodes n ON n.metric_id = m.id
		WHERE n.node_id = ? AND m.enabled = 1 ORDER BY m.name ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("querying custom metrics for node: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var metrics []model.CustomMetric
	for rows.Next() {
		var metric model.CustomMetric
		if err := rows.Scan(
			&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds,
			&metric.Description, &metric.Enabled, &metric.CreatedAt, &metric.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning custom metric for node: %w", err)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating custom metrics for node: %w", err)
	}
	return metrics, nil
}

// SetNodeBindings replaces the set of custom metrics bound to a node.
func (s *CustomMetricStore) SetNodeBindings(ctx context.Context, nodeID string, metricIDs []string) error {
	if len(metricIDs) > maxCustomMetricTargets {
		return fmt.Errorf("%w: at most 1024 metric bindings", ErrCustomMetricLimit)
	}
	ctx, cancel := context.WithTimeout(ctx, customMetricChangeTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return fmt.Errorf("begin custom metric bindings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "DELETE FROM custom_metric_nodes WHERE node_id = ?", nodeID); err != nil {
		return fmt.Errorf("clearing custom metric bindings: %w", err)
	}
	for _, metricID := range metricIDs {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO custom_metric_nodes (metric_id, node_id) VALUES (?, ?)",
			metricID, nodeID,
		); err != nil {
			return fmt.Errorf("inserting custom metric binding: %w", err)
		}
	}
	if _, err := queueCustomMetricRegistration(ctx, tx, nodeID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit custom metric bindings: %w", err)
	}
	return nil
}

// NodeMetricIDs returns the custom metric IDs bound to a node.
func (s *CustomMetricStore) NodeMetricIDs(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT metric_id FROM custom_metric_nodes WHERE node_id = ? ORDER BY metric_id", nodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying custom metric bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning custom metric binding: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating custom metric bindings: %w", err)
	}
	return ids, nil
}

// NodeIDsForMetric returns the node IDs a custom metric is bound to.
func (s *CustomMetricStore) NodeIDsForMetric(ctx context.Context, metricID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT node_id FROM custom_metric_nodes WHERE metric_id = ? ORDER BY node_id", metricID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying custom metric nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning custom metric node: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating custom metric nodes: %w", err)
	}
	return ids, nil
}

// SetMetricNodes replaces the set of nodes a custom metric is bound to.
func (s *CustomMetricStore) SetMetricNodes(ctx context.Context, id string, nodeIDs []string) error {
	_, err := s.SetMetricNodesWithRegistration(ctx, id, nodeIDs)
	return err
}

func validateCustomMetric(metric model.CustomMetric) error {
	name := strings.TrimSpace(metric.Name)
	if name == "" || len(name) > 100 || name != metric.Name || !validStoredCustomMetricName(name) {
		return fmt.Errorf("%w: name must be between 1 and 100 characters", ErrCustomMetricInvalid)
	}
	command := strings.TrimSpace(metric.ShellCommand)
	if command == "" || len(metric.ShellCommand) > 2048 || strings.ContainsRune(metric.ShellCommand, 0) {
		return fmt.Errorf("%w: shell_command must be between 1 and 2048 characters", ErrCustomMetricInvalid)
	}
	if metric.IntervalSeconds < 5 || metric.IntervalSeconds > 86400 {
		return fmt.Errorf("%w: interval_seconds must be between 5 and 86400", ErrCustomMetricInvalid)
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validStoredCustomMetricName(name string) bool {
	for _, character := range name {
		if !validStoredCustomMetricCharacter(character) {
			return false
		}
	}
	return true
}

func validStoredCustomMetricCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_' || character == '.' || character == '-'
}
