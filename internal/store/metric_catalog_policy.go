package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

type MetricCatalogPolicyChange struct {
	Policy    model.MetricPolicyUpdate
	Audit     model.AdminAuditEvent
	SessionID string
}

type MetricCatalogChange struct {
	Definition *model.MetricDefinition
	Tasks      []model.AgentTask
}

func (catalog *MetricCatalogStore) UpdatePolicy(ctx context.Context, key string, change MetricCatalogPolicyChange) (MetricCatalogChange, error) {
	ctx, cancel := context.WithTimeout(ctx, metricCatalogWriteTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, catalog.db)
	if err != nil {
		return MetricCatalogChange{}, err
	}
	defer func() { _ = tx.Rollback() }()
	mutation := metricPolicyMutation{key: key, change: change}
	if err := prepareMetricPolicy(ctx, tx, &mutation); err != nil {
		return MetricCatalogChange{}, err
	}
	result, err := applyMetricPolicy(ctx, tx, mutation)
	if err != nil {
		return MetricCatalogChange{}, err
	}
	if err := tx.Commit(); err != nil {
		return MetricCatalogChange{}, err
	}
	return result, nil
}

type metricPolicyMutation struct {
	key      string
	change   MetricCatalogPolicyChange
	previous *model.MetricDefinition
	next     model.MetricDefinition
}

func prepareMetricPolicy(ctx context.Context, tx schemaExecutor, mutation *metricPolicyMutation) error {
	change := &mutation.change
	if err := authorizeMetricPolicy(ctx, tx, change); err != nil {
		return err
	}
	previous, err := findMetricDefinition(ctx, tx, mutation.key)
	if err != nil {
		return err
	}
	if previous == nil {
		return ErrMetricCatalogNotFound
	}
	if previous.Revision != change.Policy.Revision {
		return ErrMetricCatalogConflict
	}
	next, err := metriccatalog.ApplyPolicy(*previous, change.Policy)
	if err != nil {
		return err
	}
	if err := validateMetricDependencies(ctx, tx, next); err != nil {
		return err
	}
	mutation.previous, mutation.next = previous, next
	return nil
}

func applyMetricPolicy(ctx context.Context, tx schemaExecutor, mutation metricPolicyMutation) (MetricCatalogChange, error) {
	if err := writeMetricPolicy(ctx, tx, metricPolicyWrite{definition: mutation.next, actor: mutation.change.Audit.ActorID}); err != nil {
		return MetricCatalogChange{}, err
	}
	tasks, err := metricPolicyRegistrationTasks(ctx, tx, mutation.next)
	if err != nil {
		return MetricCatalogChange{}, err
	}
	updated, err := findMetricDefinition(ctx, tx, mutation.key)
	if err != nil {
		return MetricCatalogChange{}, err
	}
	if err := auditMetricPolicy(ctx, tx, metricPolicyAuditChange{previous: *mutation.previous, next: *updated, event: mutation.change.Audit}); err != nil {
		return MetricCatalogChange{}, err
	}
	return MetricCatalogChange{Definition: updated, Tasks: tasks}, nil
}

type metricPolicyWrite struct {
	definition model.MetricDefinition
	actor      string
}

func writeMetricPolicy(ctx context.Context, db schemaExecutor, write metricPolicyWrite) error {
	definition := write.definition
	now := model.NowUTC()
	result, err := db.ExecContext(ctx, `UPDATE metric_definitions SET label=?,description=?,default_aggregation=?,public=?,enabled=?,
		retention_days=?,revision=revision+1,updated_at=?,updated_by=? WHERE metric_key=? AND revision=?`, definition.Label, definition.Description,
		definition.DefaultAggregation, boolInt(definition.Public), boolInt(definition.Enabled), definition.RetentionOverride, now, write.actor, definition.Key, definition.Revision)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrMetricCatalogConflict
	}
	_, err = db.ExecContext(ctx, `INSERT INTO metric_retention_jobs(metric_key,revision,state,requested_at)
		VALUES(?,?,'queued',?) ON CONFLICT(metric_key) DO UPDATE SET revision=excluded.revision,state='queued',
		requested_at=excluded.requested_at,next_attempt_at=NULL,attempts=0,last_error=''`, definition.Key, definition.Revision+1, now)
	return err
}

func metricPolicyRegistrationTasks(ctx context.Context, db schemaExecutor, definition model.MetricDefinition) ([]model.AgentTask, error) {
	if definition.Source != "custom" {
		return nil, nil
	}
	var metricID string
	err := db.QueryRowContext(ctx, "SELECT id FROM custom_metrics WHERE name=?", strings.TrimPrefix(definition.Key, model.CustomMetricPrefix)).Scan(&metricID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	nodes, err := customMetricNodeIDs(ctx, db, metricID)
	if err != nil {
		return nil, err
	}
	tasks := make([]model.AgentTask, 0, len(nodes))
	for _, nodeID := range nodes {
		task, err := queueCustomMetricRegistration(ctx, db, nodeID)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

type metricPolicyAuditChange struct {
	previous, next model.MetricDefinition
	event          model.AdminAuditEvent
}

func auditMetricPolicy(ctx context.Context, db schemaExecutor, change metricPolicyAuditChange) error {
	detail, err := json.Marshal(struct {
		Before model.MetricDefinition `json:"before"`
		After  model.MetricDefinition `json:"after"`
	}{change.previous, change.next})
	if err != nil {
		return err
	}
	event := change.event
	event.ID, event.Action, event.ResourceType = uuid.NewString(), "metric.catalog.update", "metric_definition"
	event.ResourceID, event.Outcome = change.next.Key, model.AuditOutcomeSuccess
	event.DetailJSON, event.CreatedAt = string(detail), model.NowUTC()
	return insertAuditEvent(ctx, db, &event)
}
