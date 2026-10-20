package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func (s *AlertEvaluationRuleStore) Create(ctx context.Context, change AlertRuleMutation) (*model.AlertEvaluationRule, error) {
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateEventActor(ctx, tx, change.Actor); err != nil {
		return nil, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	rule := change.Rule
	rule.ID, rule.Revision, rule.DeletedAt = id.String(), 1, nil
	rule.NumericPolicyLocked = false
	rule.Operator = alerteval.CanonicalOperator(rule.Operator)
	rule.CreatedAt, rule.UpdatedAt = model.NowUTC(), model.NowUTC()
	if err := validateAdvancedAlertRule(ctx, tx, rule); err != nil {
		return nil, err
	}
	values := advancedAlertRuleValues(rule)
	if _, err := tx.ExecContext(ctx, "INSERT INTO alert_rules ("+advancedAlertRuleColumns+") VALUES ("+sqlPlaceholders(len(values))+")", values...); err != nil {
		return nil, err
	}
	if err := replaceAlertRuleScope(ctx, tx, rule); err != nil {
		return nil, err
	}
	if err := checkAlertRuleCapacity(ctx, tx); err != nil {
		return nil, err
	}
	if err := auditAlertRule(ctx, tx, alertRuleAudit{rule: rule, actor: change.Actor, action: "create"}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *AlertEvaluationRuleStore) Update(ctx context.Context, id string, change AlertRuleMutation) (*model.AlertEvaluationRule, error) {
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateEventActor(ctx, tx, change.Actor); err != nil {
		return nil, err
	}
	previous, err := findAdvancedAlertRule(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if previous.Revision != change.ExpectedRevision || previous.DeletedAt != nil {
		return nil, alerteval.ErrConflict
	}
	rule := change.Rule
	rule.ID, rule.Revision, rule.DeletedAt = id, previous.Revision+1, nil
	rule.NumericPolicyLocked = previous.NumericPolicyLocked
	rule.CreatedAt, rule.UpdatedAt = previous.CreatedAt, model.NowUTC()
	rule.Operator = alerteval.CanonicalOperator(rule.Operator)
	if err := validateAdvancedAlertRule(ctx, tx, rule); err != nil {
		return nil, err
	}
	if err := guardActiveAlertPolicy(ctx, tx, activeAlertPolicy{previous: *previous, next: rule}); err != nil {
		return nil, err
	}
	if err := persistAdvancedAlertRule(ctx, tx, rule); err != nil {
		return nil, err
	}
	if err := replaceAlertRuleScope(ctx, tx, rule); err != nil {
		return nil, err
	}
	if err := checkAlertRuleCapacity(ctx, tx); err != nil {
		return nil, err
	}
	if err := auditAlertRule(ctx, tx, alertRuleAudit{rule: rule, actor: change.Actor, action: "update"}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *AlertEvaluationRuleStore) Delete(ctx context.Context, id string, change AlertRuleMutation) error {
	ctx, cancel := context.WithTimeout(ctx, alerteval.CommitTimeout)
	defer cancel()
	tx, err := beginWriteTx(ctx, s.db)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateEventActor(ctx, tx, change.Actor); err != nil {
		return err
	}
	rule, err := findAdvancedAlertRule(ctx, tx, id)
	if err != nil {
		return err
	}
	if rule.Revision != change.ExpectedRevision || rule.DeletedAt != nil {
		return alerteval.ErrConflict
	}
	now := model.NowUTC()
	result, err := tx.ExecContext(ctx, "UPDATE alert_rules SET enabled=0,deleted_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?", now.UnixMilli(), now, id, rule.Revision)
	if changed, err := eventMutationResult(result, err); err != nil || !changed {
		if err != nil {
			return err
		}
		return alerteval.ErrConflict
	}
	if err := auditAlertRule(ctx, tx, alertRuleAudit{rule: *rule, actor: change.Actor, action: "delete"}); err != nil {
		return err
	}
	return tx.Commit()
}

func persistAdvancedAlertRule(ctx context.Context, tx *sql.Tx, rule model.AlertEvaluationRule) error {
	columns := strings.Split(strings.ReplaceAll(advancedAlertRuleColumns, "\n\t", ""), ",")[1:]
	for index := range columns {
		columns[index] = strings.TrimSpace(columns[index]) + "=?"
	}
	values := append(advancedAlertRuleValues(rule)[1:], rule.ID, rule.Revision-1)
	result, err := tx.ExecContext(ctx, "UPDATE alert_rules SET "+strings.Join(columns, ",")+" WHERE id=? AND revision=?", values...)
	if updated, err := eventMutationResult(result, err); err != nil || !updated {
		if err != nil {
			return err
		}
		return alerteval.ErrConflict
	}
	return nil
}
