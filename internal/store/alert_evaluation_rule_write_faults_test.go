package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

// alertRuleCommitFault 用延迟外键让规则写入在提交期失败。
func alertRuleCommitFault(t *testing.T, sqlite *SQLiteStore, target string) {
	t.Helper()
	alertRuleTrigger(t, sqlite, `CREATE TABLE cov_alert_rule_child(
		rule TEXT REFERENCES alert_rules(id) DEFERRABLE INITIALLY DEFERRED)`)
	alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_alert_rule_commit AFTER `+target+
		` BEGIN INSERT INTO cov_alert_rule_child VALUES('missing-rule'); END`)
}

func alertRuleUpdate(change AlertRuleMutation, created *model.AlertEvaluationRule) AlertRuleMutation {
	update := change
	update.Rule, update.ExpectedRevision = *created, created.Revision
	return update
}

// TestAlertRuleCreateWriteFaults 覆盖规则创建的事务、身份、作用域与提交失败。
func TestAlertRuleCreateWriteFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		_, _, change := evaluationRuleFixture(t)
		if _, err := NewAlertEvaluationRuleStore(closedSQLiteStore(t).DB).Create(t.Context(), change); err == nil {
			t.Fatal("closed database created a rule")
		}
	})
	t.Run("actor rejected", func(t *testing.T) {
		_, rules, change := evaluationRuleFixture(t)
		change.Actor.SessionID = "missing-session"
		if _, err := rules.Create(t.Context(), change); !errors.Is(err, ErrAdminSessionNotFound) {
			t.Fatalf("rejected actor error = %v", err)
		}
	})
	t.Run("scope insert blocked", func(t *testing.T) {
		sqlite, rules, change := evaluationRuleFixture(t)
		node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "cov-rule-scope", "192.0.2.9", 22)
		if err != nil {
			t.Fatal(err)
		}
		change.Rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{node.ID}}
		alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_rule_scope_insert BEFORE INSERT ON alert_rule_nodes
			BEGIN SELECT RAISE(ABORT,'scope blocked'); END`)
		if _, err := rules.Create(t.Context(), change); err == nil {
			t.Fatal("blocked rule scope was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		sqlite, rules, change := evaluationRuleFixture(t)
		alertRuleCommitFault(t, sqlite, "INSERT ON alert_rules")
		if _, err := rules.Create(t.Context(), change); err == nil {
			t.Fatal("deferred rule commit failure was accepted")
		}
	})
}

// TestAlertRuleUpdateWriteFaults 覆盖规则更新的事务、读取、校验、作用域、审计与提交失败。
func TestAlertRuleUpdateWriteFaults(t *testing.T) {
	t.Run("closed database", func(t *testing.T) {
		_, _, change, created := createdEvaluationRule(t)
		update := alertRuleUpdate(change, created)
		if _, err := NewAlertEvaluationRuleStore(closedSQLiteStore(t).DB).Update(t.Context(),
			created.ID, update); err == nil {
			t.Fatal("closed database updated a rule")
		}
	})
	t.Run("rule lookup failed", func(t *testing.T) {
		sqlite, rules, change, created := createdEvaluationRule(t)
		update := alertRuleUpdate(change, created)
		alertRuleTrigger(t, sqlite, "ALTER TABLE alert_rules RENAME TO cov_alert_rules_backup")
		if _, err := rules.Update(t.Context(), created.ID, update); err == nil {
			t.Fatal("unreadable rule ledger was accepted")
		}
	})
	t.Run("invalid rule", func(t *testing.T) {
		_, rules, change, created := createdEvaluationRule(t)
		update := alertRuleUpdate(change, created)
		update.Rule.Metric = "cov-missing-metric"
		if _, err := rules.Update(t.Context(), created.ID, update); err == nil {
			t.Fatal("unknown metric rule was updated")
		}
	})
	t.Run("scope insert blocked", func(t *testing.T) {
		sqlite, rules, change, created := createdEvaluationRule(t)
		update := alertRuleUpdate(change, created)
		node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "cov-rule-update-scope", "192.0.2.11", 22)
		if err != nil {
			t.Fatal(err)
		}
		update.Rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{node.ID}}
		alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_rule_scope_update BEFORE INSERT ON alert_rule_nodes
			BEGIN SELECT RAISE(ABORT,'scope blocked'); END`)
		if _, err := rules.Update(t.Context(), created.ID, update); err == nil {
			t.Fatal("blocked rule scope update was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		sqlite, rules, change, created := createdEvaluationRule(t)
		update := alertRuleUpdate(change, created)
		alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_rule_update_audit BEFORE INSERT ON admin_audit_events
			BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if _, err := rules.Update(t.Context(), created.ID, update); err == nil {
			t.Fatal("unchecked rule update audit was accepted")
		}
	})
	t.Run("commit blocked", func(t *testing.T) {
		sqlite, rules, change, created := createdEvaluationRule(t)
		update := alertRuleUpdate(change, created)
		alertRuleCommitFault(t, sqlite, "UPDATE ON alert_rules")
		if _, err := rules.Update(t.Context(), created.ID, update); err == nil {
			t.Fatal("deferred rule update commit failure was accepted")
		}
	})
}

// TestAlertRuleDeleteWriteFaults 覆盖规则删除的事务、身份、读取与审计失败。
func TestAlertRuleDeleteWriteFaults(t *testing.T) {
	mutation := func(change AlertRuleMutation, created *model.AlertEvaluationRule) AlertRuleMutation {
		return AlertRuleMutation{Rule: *created, Actor: change.Actor, ExpectedRevision: created.Revision}
	}
	t.Run("closed database", func(t *testing.T) {
		_, _, change, created := createdEvaluationRule(t)
		if err := NewAlertEvaluationRuleStore(closedSQLiteStore(t).DB).Delete(t.Context(),
			created.ID, mutation(change, created)); err == nil {
			t.Fatal("closed database deleted a rule")
		}
	})
	t.Run("actor rejected", func(t *testing.T) {
		_, rules, change, created := createdEvaluationRule(t)
		rejected := mutation(change, created)
		rejected.Actor.SessionID = "missing-session"
		if err := rules.Delete(t.Context(), created.ID, rejected); !errors.Is(err, ErrAdminSessionNotFound) {
			t.Fatalf("rejected actor error = %v", err)
		}
	})
	t.Run("rule lookup failed", func(t *testing.T) {
		sqlite, rules, change, created := createdEvaluationRule(t)
		alertRuleTrigger(t, sqlite, "ALTER TABLE alert_rules RENAME TO cov_alert_rules_backup")
		if err := rules.Delete(t.Context(), created.ID, mutation(change, created)); err == nil {
			t.Fatal("unreadable rule ledger delete was accepted")
		}
	})
	t.Run("audit blocked", func(t *testing.T) {
		sqlite, rules, change, created := createdEvaluationRule(t)
		alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_rule_delete_audit BEFORE INSERT ON admin_audit_events
			BEGIN SELECT RAISE(ABORT,'audit blocked'); END`)
		if err := rules.Delete(t.Context(), created.ID, mutation(change, created)); err == nil {
			t.Fatal("unchecked rule delete audit was accepted")
		}
	})
}
