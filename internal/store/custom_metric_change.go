package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	maxCustomMetricTargets       = 1024
	maxCustomMetricAffectedNodes = 2 * maxCustomMetricTargets
	customMetricChangeTimeout    = 5 * time.Second
)

var ErrCustomMetricLimit = errors.New("custom metric configuration exceeds its limit")

var ErrCustomMetricInvalid = errors.New("invalid custom metric definition")

type CustomMetricChange struct {
	Metric *model.CustomMetric
	Tasks  []model.AgentTask
}

type customMetricMutation struct {
	metricID  string
	nextNodes []string
	write     func(context.Context, schemaExecutor) (*model.CustomMetric, error)
}

func (store *CustomMetricStore) UpdateWithRegistration(ctx context.Context, id string, metric model.CustomMetric) (CustomMetricChange, error) {
	if err := validateCustomMetric(metric); err != nil {
		return CustomMetricChange{}, err
	}
	return store.applyCustomMetricChange(ctx, customMetricMutation{metricID: id, write: func(ctx context.Context, db schemaExecutor) (*model.CustomMetric, error) {
		metric.ID, metric.UpdatedAt = id, model.NowUTC()
		result, err := db.ExecContext(ctx, `UPDATE custom_metrics SET name=?,shell_command=?,interval_seconds=?,description=?,enabled=?,updated_at=? WHERE id=?`,
			metric.Name, metric.ShellCommand, metric.IntervalSeconds, metric.Description, boolInt(metric.Enabled), metric.UpdatedAt, id)
		if err != nil {
			return nil, mapCustomMetricWriteError(err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("count custom metric update: %w", err)
		}
		if count == 0 {
			return nil, nil
		}
		definition, err := customCatalogDefinition(metric.Name, metric.Description)
		if err != nil {
			return nil, err
		}
		if err := ensureMetricDefinition(ctx, db, definition); err != nil {
			return nil, err
		}
		return &metric, nil
	}})
}

func (store *CustomMetricStore) DeleteWithRegistration(ctx context.Context, id string) (CustomMetricChange, error) {
	return store.applyCustomMetricChange(ctx, customMetricMutation{metricID: id, write: func(ctx context.Context, db schemaExecutor) (*model.CustomMetric, error) {
		_, err := db.ExecContext(ctx, "DELETE FROM custom_metrics WHERE id=?", id)
		return nil, err
	}})
}

func (store *CustomMetricStore) SetMetricNodesWithRegistration(ctx context.Context, id string, nodeIDs []string) (CustomMetricChange, error) {
	if len(nodeIDs) > maxCustomMetricTargets {
		return CustomMetricChange{}, fmt.Errorf("%w: at most 1024 target nodes", ErrCustomMetricLimit)
	}
	return store.applyCustomMetricChange(ctx, customMetricMutation{metricID: id, nextNodes: nodeIDs, write: func(ctx context.Context, db schemaExecutor) (*model.CustomMetric, error) {
		if _, err := db.ExecContext(ctx, "DELETE FROM custom_metric_nodes WHERE metric_id=?", id); err != nil {
			return nil, err
		}
		for _, nodeID := range nodeIDs {
			if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO custom_metric_nodes(metric_id,node_id) VALUES(?,?)", id, nodeID); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}})
}

func (store *CustomMetricStore) applyCustomMetricChange(ctx context.Context, mutation customMetricMutation) (CustomMetricChange, error) {
	ctx, cancel := context.WithTimeout(ctx, customMetricChangeTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return CustomMetricChange{}, fmt.Errorf("begin custom metric change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	previous, err := customMetricNodeIDs(ctx, tx, mutation.metricID)
	if err != nil {
		return CustomMetricChange{}, err
	}
	metric, err := mutation.write(ctx, tx)
	if err != nil {
		return CustomMetricChange{}, err
	}
	nodes, err := customMetricAffectedNodes(previous, mutation.nextNodes)
	if err != nil {
		return CustomMetricChange{}, err
	}
	tasks := make([]model.AgentTask, 0, len(nodes))
	for _, nodeID := range nodes {
		task, err := queueCustomMetricRegistration(ctx, tx, nodeID)
		if err != nil {
			return CustomMetricChange{}, err
		}
		tasks = append(tasks, *task)
	}
	if err := tx.Commit(); err != nil {
		return CustomMetricChange{}, fmt.Errorf("commit custom metric change: %w", err)
	}
	return CustomMetricChange{Metric: metric, Tasks: tasks}, nil
}

func customMetricNodeIDs(ctx context.Context, db schemaExecutor, id string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT node_id FROM custom_metric_nodes WHERE metric_id=? ORDER BY node_id LIMIT ?", id, maxCustomMetricAffectedNodes+1)
	if err != nil {
		return nil, fmt.Errorf("read previous custom metric bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	nodes := make([]string, 0)
	for rows.Next() {
		var node string
		if err := rows.Scan(&node); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(nodes) > maxCustomMetricAffectedNodes {
		return nil, ErrCustomMetricLimit
	}
	return nodes, nil
}

func customMetricAffectedNodes(previous, current []string) ([]string, error) {
	seen := make(map[string]bool, len(previous)+len(current))
	for _, group := range [][]string{previous, current} {
		for _, id := range group {
			seen[id] = true
		}
	}
	if len(seen) > maxCustomMetricAffectedNodes {
		return nil, ErrCustomMetricLimit
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func mapCustomMetricWriteError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrCustomMetricNameTaken
	}
	return err
}

// 保持事务执行器的编译期约束，元数据与待发送快照使用同一SQLite事务。
var _ schemaExecutor = (*sql.Tx)(nil)
