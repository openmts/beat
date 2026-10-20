package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func authorizeEvaluationCommit(ctx context.Context, tx *sql.Tx, change AlertEvaluationCommit) error {
	state := change.Decision.State
	rule, err := findAdvancedAlertRule(ctx, tx, state.RuleID)
	if err != nil {
		return err
	}
	if rule.Revision != change.RuleRevision || state.RuleRevision != rule.Revision {
		return alerteval.ErrConflict
	}
	if !rule.Enabled || rule.DeletedAt != nil {
		if state.Status == model.EvaluationDisabled && len(change.Decision.Actions) == 0 {
			return nil
		}
		return alerteval.ErrConflict
	}
	matched, err := evaluationScopeMatches(ctx, tx, evaluationScopeSelection{rule: rule, nodeID: state.NodeID, deviceID: state.DeviceID})
	if err != nil {
		return err
	}
	if !matched {
		if state.Status == model.EvaluationOutOfScope && len(change.Decision.Actions) == 0 {
			return nil
		}
		return alerteval.ErrConflict
	}
	if !expectedEvaluationPolicies(*rule, change.Policies) {
		return alerteval.ErrConflict
	}
	if model.IsAcceleratorAlertMetric(rule.Metric) {
		if err := recheckAcceleratorEvaluation(ctx, tx, change); err != nil {
			return err
		}
	}
	return recheckEvaluationMetricPolicies(ctx, tx, change)
}

type evaluationScopeSelection struct {
	rule     *model.AlertEvaluationRule
	nodeID   string
	deviceID string
}

func evaluationScopeMatches(ctx context.Context, db schemaExecutor, selection evaluationScopeSelection) (bool, error) {
	if model.IsAcceleratorAlertMetric(selection.rule.Metric) {
		if !model.ValidAcceleratorDeviceID(selection.deviceID) || (selection.rule.Scope.DeviceID != model.AcceleratorAnyDevice && selection.rule.Scope.DeviceID != selection.deviceID) {
			return false, nil
		}
	} else if selection.deviceID != "" {
		return false, nil
	}
	query := "SELECT EXISTS(SELECT 1 FROM nodes WHERE id=?"
	arguments := []any{selection.nodeID}
	switch selection.rule.Scope.Kind {
	case "nodes":
		query += " AND id IN (SELECT node_id FROM alert_rule_nodes WHERE rule_id=?)"
		arguments = append(arguments, selection.rule.ID)
	case "groups":
		query += " AND group_id IN (SELECT group_id FROM alert_rule_groups WHERE rule_id=?)"
		arguments = append(arguments, selection.rule.ID)
	}
	query += ")"
	var matches bool
	err := db.QueryRowContext(ctx, query, arguments...).Scan(&matches)
	return matches, err
}

func recheckEvaluationMetricPolicies(ctx context.Context, db schemaExecutor, change AlertEvaluationCommit) error {
	for _, reference := range change.Policies {
		definition, err := findMetricDefinition(ctx, db, reference.Key)
		if err != nil {
			return err
		}
		if definition == nil || definition.Revision != reference.Revision {
			return alerteval.ErrConflict
		}
		if !definition.Enabled {
			if change.Decision.State.Status == model.EvaluationDisabled && len(change.Decision.Actions) == 0 {
				continue
			}
			return alerteval.ErrConflict
		}
		if strings.HasPrefix(reference.Key, model.CustomMetricPrefix) {
			if err := recheckCustomExecutionPolicy(ctx, db, customExecutionReference{nodeID: change.Decision.State.NodeID, reference: reference,
				allowMissing: change.Decision.State.Status == model.EvaluationOutOfScope && change.Decision.State.Reason == "custom_unbound" && len(change.Decision.Actions) == 0}); err != nil {
				return err
			}
		}
	}
	return nil
}

type customExecutionReference struct {
	nodeID       string
	reference    AlertMetricPolicyReference
	allowMissing bool
}

func recheckCustomExecutionPolicy(ctx context.Context, db schemaExecutor, input customExecutionReference) error {
	var metric model.CustomMetric
	err := db.QueryRowContext(ctx, `SELECT m.id,m.name,m.shell_command,m.interval_seconds,m.updated_at FROM custom_metrics m
		JOIN custom_metric_nodes n ON n.metric_id=m.id WHERE n.node_id=? AND m.name=? AND m.enabled=1`, input.nodeID, strings.TrimPrefix(input.reference.Key, model.CustomMetricPrefix)).
		Scan(&metric.ID, &metric.Name, &metric.ShellCommand, &metric.IntervalSeconds, &metric.UpdatedAt)
	if err == sql.ErrNoRows {
		if input.allowMissing && input.reference.DefinitionVersion == "" && input.reference.CadenceMS == 0 {
			return nil
		}
		return alerteval.ErrConflict
	}
	if err != nil {
		return err
	}
	if input.reference.CadenceMS != int64(metric.IntervalSeconds)*1000 || input.reference.DefinitionVersion != model.CustomMetricDefinitionVersion(metric) {
		return alerteval.ErrConflict
	}
	return nil
}

func expectedEvaluationPolicies(rule model.AlertEvaluationRule, references []AlertMetricPolicyReference) bool {
	required := model.AcceleratorAlertInputs(rule.Metric)
	if rule.Metric == model.MetricHeartbeatAgeSeconds {
		required = nil
	}
	if rule.Metric == model.MetricTrafficUsagePercent {
		required = []string{"net_recv_delta", "net_sent_delta"}
	}
	if len(required) != len(references) {
		return false
	}
	for _, key := range required {
		count := 0
		for _, reference := range references {
			if reference.Key == key && reference.Revision > 0 {
				count++
			}
		}
		if count != 1 {
			return false
		}
	}
	return true
}
