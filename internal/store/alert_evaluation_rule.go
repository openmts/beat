package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

var ErrAlertEvaluationRuleNotFound = errors.New("alert evaluation rule not found")

type AlertEvaluationRuleStore struct{ db *sql.DB }

type AlertRuleMutation struct {
	Rule             model.AlertEvaluationRule
	ExpectedRevision int64
	Actor            EventMutationActor
}

func NewAlertEvaluationRuleStore(db *sql.DB) *AlertEvaluationRuleStore {
	return &AlertEvaluationRuleStore{db: db}
}

const advancedAlertRuleColumns = `id,name,description,metric,operator,threshold,duration,severity,enabled,created_at,updated_at,
	revision,deleted_at,scope_kind,window_seconds,aggregation,recovery_threshold,recovery_duration,no_data_policy,no_data_delay_seconds,error_policy,error_delay_seconds,device_id`

func (s *AlertEvaluationRuleStore) Get(ctx context.Context, id string) (*model.AlertEvaluationRule, error) {
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rule, err := findAdvancedAlertRule(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rule, nil
}

func findAdvancedAlertRule(ctx context.Context, db schemaExecutor, id string) (*model.AlertEvaluationRule, error) {
	rule, err := scanAdvancedAlertRule(db.QueryRowContext(ctx, "SELECT "+advancedAlertRuleColumns+" FROM alert_rules WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAlertEvaluationRuleNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := readAlertRuleScope(ctx, db, rule); err != nil {
		return nil, err
	}
	if err := readAlertRuleLock(ctx, db, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func scanAdvancedAlertRule(row metricDefinitionScanner) (*model.AlertEvaluationRule, error) {
	var rule model.AlertEvaluationRule
	var deleted sql.NullInt64
	err := row.Scan(&rule.ID, &rule.Name, &rule.Description, &rule.Metric, &rule.Operator, &rule.Threshold, &rule.Duration, &rule.Severity, &rule.Enabled, &rule.CreatedAt, &rule.UpdatedAt,
		&rule.Revision, &deleted, &rule.Scope.Kind, &rule.WindowSeconds, &rule.Aggregation, &rule.RecoveryThreshold, &rule.RecoveryDuration, &rule.NoDataPolicy, &rule.NoDataDelaySeconds, &rule.ErrorPolicy, &rule.ErrorDelaySeconds, &rule.Scope.DeviceID)
	if err != nil {
		return nil, err
	}
	if deleted.Valid {
		value := time.UnixMilli(deleted.Int64).UTC()
		rule.DeletedAt = &value
	}
	return &rule, nil
}

func readAlertRuleScope(ctx context.Context, db schemaExecutor, rule *model.AlertEvaluationRule) error {
	table, column := "alert_rule_nodes", "node_id"
	if rule.Scope.Kind == "all" {
		return nil
	}
	if rule.Scope.Kind == "groups" {
		table, column = "alert_rule_groups", "group_id"
	}
	rows, err := db.QueryContext(ctx, "SELECT "+column+" FROM "+table+" WHERE rule_id=? ORDER BY "+column, rule.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		if len(ids) > alerteval.MaximumScopeNodes {
			return alerteval.ErrCapacity
		}
	}
	if rule.Scope.Kind == "nodes" {
		rule.Scope.NodeIDs = ids
	} else {
		rule.Scope.GroupIDs = ids
	}
	return rows.Err()
}

func advancedAlertRuleValues(rule model.AlertEvaluationRule) []any {
	var deleted any
	if rule.DeletedAt != nil {
		deleted = rule.DeletedAt.UnixMilli()
	}
	return []any{rule.ID, rule.Name, rule.Description, rule.Metric, rule.Operator, rule.Threshold, rule.Duration, rule.Severity, rule.Enabled, rule.CreatedAt, rule.UpdatedAt,
		rule.Revision, deleted, rule.Scope.Kind, rule.WindowSeconds, rule.Aggregation, rule.RecoveryThreshold, rule.RecoveryDuration, rule.NoDataPolicy, rule.NoDataDelaySeconds, rule.ErrorPolicy, rule.ErrorDelaySeconds, rule.Scope.DeviceID}
}

func sqlPlaceholders(count int) string { return strings.TrimSuffix(strings.Repeat("?,", count), ",") }
