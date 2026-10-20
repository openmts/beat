package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

// ValidateAlertPairCapacityTx 在节点/分组归属写入后、同事务提交前调用。
func ValidateAlertPairCapacityTx(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return alerteval.ErrConflict
	}
	return checkAlertRuleCapacity(ctx, tx)
}

func validateAdvancedAlertRule(ctx context.Context, db schemaExecutor, rule model.AlertEvaluationRule) error {
	definition, err := findMetricDefinition(ctx, db, rule.Metric)
	if err != nil {
		return err
	}
	if model.AcceleratorAlertDefinition(rule.Metric) != nil {
		for _, key := range model.AcceleratorAlertInputs(rule.Metric) {
			input, err := findMetricDefinition(ctx, db, key)
			if err != nil {
				return err
			}
			if input == nil || !input.Enabled {
				return alerteval.ErrInvalidPolicy
			}
		}
	}
	if err := alerteval.ValidateRule(rule, definition); err != nil {
		return err
	}
	ids, table := rule.Scope.NodeIDs, "nodes"
	if rule.Scope.Kind == "groups" {
		ids, table = rule.Scope.GroupIDs, "groups"
	}
	if rule.Scope.Kind == "all" {
		return nil
	}
	arguments := make([]any, len(ids))
	for index, id := range ids {
		arguments[index] = id
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id IN ("+sqlPlaceholders(len(ids))+")", arguments...).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return alerteval.ErrInvalidPolicy
	}
	if model.IsAcceleratorAlertMetric(rule.Metric) && rule.Scope.DeviceID != model.AcceleratorAnyDevice {
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM node_accelerators WHERE node_id=? AND device_id=?", rule.Scope.NodeIDs[0], rule.Scope.DeviceID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return alerteval.ErrInvalidPolicy
		}
	}
	return nil
}

func replaceAlertRuleScope(ctx context.Context, db schemaExecutor, rule model.AlertEvaluationRule) error {
	for _, table := range []string{"alert_rule_nodes", "alert_rule_groups"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table+" WHERE rule_id=?", rule.ID); err != nil {
			return err
		}
	}
	ids, table, column := rule.Scope.NodeIDs, "alert_rule_nodes", "node_id"
	if rule.Scope.Kind == "groups" {
		ids, table, column = rule.Scope.GroupIDs, "alert_rule_groups", "group_id"
	}
	for _, id := range ids {
		if _, err := db.ExecContext(ctx, "INSERT INTO "+table+"(rule_id,"+column+") VALUES(?,?)", rule.ID, id); err != nil {
			return err
		}
	}
	return nil
}

func checkAlertRuleCapacity(ctx context.Context, db schemaExecutor) error {
	var count, pairs int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM alert_rules WHERE deleted_at IS NULL").Scan(&count); err != nil {
		return err
	}
	if count > alerteval.MaximumRules {
		return alerteval.ErrCapacity
	}
	err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN device_id<>'' THEN (CASE WHEN device_id='*' THEN 32 ELSE 1 END) * (SELECT COUNT(*) FROM alert_rule_nodes b JOIN nodes n ON n.id=b.node_id WHERE b.rule_id=r.id AND n.credential_state<>'pending_claim') ELSE CASE scope_kind WHEN 'all' THEN (SELECT COUNT(*) FROM nodes WHERE credential_state<>'pending_claim')
		WHEN 'nodes' THEN (SELECT COUNT(*) FROM alert_rule_nodes b JOIN nodes n ON n.id=b.node_id WHERE b.rule_id=r.id AND n.credential_state<>'pending_claim')
		ELSE (SELECT COUNT(*) FROM nodes n WHERE n.credential_state<>'pending_claim' AND n.group_id IN (SELECT group_id FROM alert_rule_groups b WHERE b.rule_id=r.id)) END END),0)
		FROM alert_rules r WHERE r.deleted_at IS NULL AND r.enabled=1`).Scan(&pairs)
	if err != nil {
		return err
	}
	if pairs > alerteval.MaximumPairs {
		return alerteval.ErrCapacity
	}
	return nil
}

type activeAlertPolicy struct{ previous, next model.AlertEvaluationRule }

func guardActiveAlertPolicy(ctx context.Context, db schemaExecutor, policy activeAlertPolicy) error {
	first, second := policy.previous, policy.next
	if first.Metric == second.Metric && alerteval.CanonicalOperator(first.Operator) == second.Operator && first.Threshold == second.Threshold && first.Duration == second.Duration &&
		first.WindowSeconds == second.WindowSeconds && first.Aggregation == second.Aggregation && first.RecoveryDuration == second.RecoveryDuration && effectiveRecovery(first) == effectiveRecovery(second) {
		return nil
	}
	var active bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM alert_events WHERE rule_id=? AND status IN ('triggered','acknowledged'))", first.ID).Scan(&active); err != nil {
		return err
	}
	if active {
		return alerteval.ErrPolicyInUse
	}
	return nil
}

func effectiveRecovery(rule model.AlertEvaluationRule) float64 {
	if rule.RecoveryThreshold != nil {
		return *rule.RecoveryThreshold
	}
	return rule.Threshold
}

type alertRuleAudit struct {
	rule   model.AlertEvaluationRule
	actor  EventMutationActor
	action string
}

func auditAlertRule(ctx context.Context, db schemaExecutor, change alertRuleAudit) error {
	event := model.AdminAuditEvent{ID: uuid.NewString(), ActorID: change.actor.UserID, RequestID: change.actor.RequestID,
		IPAddress: change.actor.IPAddress, UserAgent: change.actor.UserAgent, Action: "alert_rule." + change.action, ResourceType: "alert_rule", ResourceID: change.rule.ID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: "{}", CreatedAt: model.NowUTC()}
	if err := db.QueryRowContext(ctx, "SELECT u.username,s.token_prefix FROM admin_users u JOIN admin_sessions s ON s.user_id=u.id WHERE s.id=? AND u.id=?", change.actor.SessionID, change.actor.UserID).Scan(&event.ActorUsername, &event.SessionPrefix); err != nil {
		return err
	}
	return insertAuditEvent(ctx, db, &event)
}
