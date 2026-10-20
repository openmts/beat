package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func alertRuleTrigger(t *testing.T, sqlite *SQLiteStore, statement string) {
	t.Helper()
	if _, err := sqlite.DB.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func createdEvaluationRule(t *testing.T) (*SQLiteStore, *AlertEvaluationRuleStore, AlertRuleMutation, *model.AlertEvaluationRule) {
	t.Helper()
	sqlite, rules, change := evaluationRuleFixture(t)
	created, err := rules.Create(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	return sqlite, rules, change, created
}

// TestAlertRuleCreateFailsClosedOnStorageFaults 覆盖创建规则时规则表或审计表
// 写入失败必须整体回滚，不允许出现无审计的规则。
func TestAlertRuleCreateFailsClosedOnStorageFaults(t *testing.T) {
	sqlite, rules, change := evaluationRuleFixture(t)
	alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_block_rule_insert BEFORE INSERT ON alert_rules
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if _, err := rules.Create(t.Context(), change); err == nil {
		t.Fatal("被阻断的规则插入被接受")
	}
	alertRuleTrigger(t, sqlite, "DROP TRIGGER cov_block_rule_insert")
	alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_block_rule_audit BEFORE INSERT ON admin_audit_events
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	if _, err := rules.Create(t.Context(), change); err == nil {
		t.Fatal("未审计的规则创建被接受")
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM alert_rules").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("失败创建留下 %d 条规则", count)
	}
}

// TestAlertRuleUpdateAndDeleteFailClosedOnStorageFaults 覆盖更新与删除的修订门禁
// 以及写入被阻断时的失败关闭。
func TestAlertRuleUpdateAndDeleteFailClosedOnStorageFaults(t *testing.T) {
	sqlite, rules, change, created := createdEvaluationRule(t)
	update := change
	update.Rule, update.ExpectedRevision = *created, created.Revision+1
	if _, err := rules.Update(t.Context(), created.ID, update); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("过期修订更新 = %v", err)
	}
	alertRuleTrigger(t, sqlite, `CREATE TRIGGER cov_block_rule_update BEFORE UPDATE ON alert_rules
		BEGIN SELECT RAISE(ABORT,'blocked'); END`)
	update.ExpectedRevision = created.Revision
	if _, err := rules.Update(t.Context(), created.ID, update); err == nil {
		t.Fatal("被阻断的规则更新被接受")
	}
	if err := rules.Delete(t.Context(), created.ID, AlertRuleMutation{Rule: *created, Actor: change.Actor,
		ExpectedRevision: created.Revision}); err == nil {
		t.Fatal("被阻断的规则删除被接受")
	}
	alertRuleTrigger(t, sqlite, "DROP TRIGGER cov_block_rule_update")
	if err := rules.Delete(t.Context(), created.ID, AlertRuleMutation{Rule: *created, Actor: change.Actor,
		ExpectedRevision: created.Revision + 5}); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("过期修订删除 = %v", err)
	}
	if err := rules.Delete(t.Context(), created.ID, AlertRuleMutation{Rule: *created, Actor: change.Actor,
		ExpectedRevision: created.Revision}); err != nil {
		t.Fatalf("删除规则 = %v", err)
	}
}

// TestAlertRuleValidationRejectsUnknownScopesAndPairCapacity 覆盖作用域校验与
// 成对容量检查的失败关闭。
func TestAlertRuleValidationRejectsUnknownScopesAndPairCapacity(t *testing.T) {
	sqlite, rules, change := evaluationRuleFixture(t)
	change.Rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{"missing-node"}}
	if _, err := rules.Create(t.Context(), change); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatalf("未知节点作用域 = %v", err)
	}
	if err := ValidateAlertPairCapacityTx(t.Context(), nil); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatalf("空事务容量检查 = %v", err)
	}
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAlertPairCapacityTx(t.Context(), tx); err == nil {
		t.Fatal("已回滚事务的容量检查被接受")
	}
}
