package store

import (
	"context"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

const missingEvaluationPairs = ` FROM alert_rules r CROSS JOIN nodes n
 WHERE r.deleted_at IS NULL AND r.enabled=1 AND r.device_id='' AND
 (r.scope_kind='all' OR (r.scope_kind='nodes' AND EXISTS(SELECT 1 FROM alert_rule_nodes b WHERE b.rule_id=r.id AND b.node_id=n.id))
 OR (r.scope_kind='groups' AND EXISTS(SELECT 1 FROM alert_rule_groups b WHERE b.rule_id=r.id AND b.group_id=n.group_id)))
 AND NOT EXISTS(SELECT 1 FROM alert_evaluation_states s WHERE s.rule_id=r.id AND s.node_id=n.id AND s.device_id='')`

const missingAcceleratorPairs = ` FROM alert_rules r JOIN alert_rule_nodes b ON b.rule_id=r.id JOIN nodes n ON n.id=b.node_id
 JOIN node_accelerators g ON g.node_id=n.id
 WHERE r.deleted_at IS NULL AND r.enabled=1 AND r.scope_kind='nodes' AND r.device_id<>''
 AND ((r.device_id='*' AND g.inactive_at IS NULL) OR r.device_id=g.device_id)
 AND NOT EXISTS(SELECT 1 FROM alert_evaluation_states s WHERE s.rule_id=r.id AND s.node_id=n.id AND s.device_id=g.device_id)`

const missingEvaluationTargets = `SELECT r.id AS rule_id,n.id AS node_id,'' AS device_id,r.revision AS rule_revision` + missingEvaluationPairs +
	` UNION ALL SELECT r.id,n.id,g.device_id,r.revision` + missingAcceleratorPairs

// Admit 建立有界持久状态；作用域变更后旧状态仍由下一轮评估标记，保留事件证据。
func (s *AlertEvaluationStore) Admit(ctx context.Context, at time.Time) (int64, error) {
	if at.IsZero() {
		return 0, alerteval.ErrInvalidPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkAlertRuleCapacity(ctx, tx); err != nil {
		return 0, err
	}
	var current, missing int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM alert_evaluation_states").Scan(&current); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+missingEvaluationTargets+")").Scan(&missing); err != nil {
		return 0, err
	}
	if current+missing > alerteval.MaximumStateRows {
		return 0, alerteval.ErrCapacity
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO alert_evaluation_states(rule_id,node_id,device_id,rule_revision,state_version,phase,evaluation_status,next_due_at,updated_at)
 SELECT rule_id,node_id,device_id,rule_revision,1,'normal','no_data',?,? FROM (`+missingEvaluationTargets+`) ORDER BY rule_id,node_id,device_id LIMIT ?`, at.UnixMilli(), at.UnixMilli(), alerteval.BatchSize)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
