package store

import (
	"context"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func (s *AlertEvaluationRuleStore) List(ctx context.Context) ([]model.AlertEvaluationRule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, "SELECT "+advancedAlertRuleColumns+" FROM alert_rules WHERE deleted_at IS NULL ORDER BY id LIMIT ?", alerteval.MaximumRules+1)
	if err != nil {
		return nil, err
	}
	rules := []model.AlertEvaluationRule{}
	for rows.Next() {
		rule, err := scanAdvancedAlertRule(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		rules = append(rules, *rule)
	}
	rowErr := rows.Err()
	closeErr := rows.Close()
	if rowErr != nil {
		return nil, rowErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(rules) > alerteval.MaximumRules {
		return nil, alerteval.ErrCapacity
	}
	for index := range rules {
		if err := readAlertRuleScope(ctx, tx, &rules[index]); err != nil {
			return nil, err
		}
		if err := readAlertRuleLock(ctx, tx, &rules[index]); err != nil {
			return nil, err
		}
	}
	return rules, tx.Commit()
}

func readAlertRuleLock(ctx context.Context, db schemaExecutor, rule *model.AlertEvaluationRule) error {
	return db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_events WHERE rule_id=? AND status IN ('triggered','acknowledged'))`, rule.ID).Scan(&rule.NumericPolicyLocked)
}

func (s *AlertEvaluationRuleStore) Validate(ctx context.Context, rule model.AlertEvaluationRule) error {
	return validateAdvancedAlertRule(ctx, s.db, rule)
}
