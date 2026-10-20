package store

import (
	"errors"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestEffectiveRecoveryPrefersExplicitThreshold(t *testing.T) {
	explicit := 12.5
	rule := model.AlertEvaluationRule{AlertRule: model.AlertRule{Threshold: 80}, RecoveryThreshold: &explicit}
	if value := effectiveRecovery(rule); value != explicit {
		t.Fatalf("explicit recovery threshold = %v", value)
	}
	fallback := model.AlertEvaluationRule{AlertRule: model.AlertRule{Threshold: 80}}
	if value := effectiveRecovery(fallback); value != 80 {
		t.Fatalf("fallback recovery threshold = %v", value)
	}
}

func TestValidateAdvancedAlertRuleRejectsBrokenInputs(t *testing.T) {
	sqlite := setupTestDB(t)
	if err := validateAdvancedAlertRule(t.Context(), covClosedHandle(t), model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: "cpu"}}); err == nil {
		t.Fatal("unreadable catalog accepted a rule")
	}
	accelerator := model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: model.MetricGPUMemoryPercent}}
	if err := validateAdvancedAlertRule(t.Context(), sqlite.DB, accelerator); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatalf("accelerator rule without derived inputs = %v", err)
	}
	invalid := model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: "cpu", Operator: "between", Threshold: 1}}
	if err := validateAdvancedAlertRule(t.Context(), sqlite.DB, invalid); err == nil {
		t.Fatal("unsupported operator accepted")
	}
	group, err := NewGroupStore(sqlite.DB).CreateGroup(t.Context(), "policy-group")
	if err != nil {
		t.Fatal(err)
	}
	grouped := alerteval.DefaultRule()
	grouped.Name, grouped.Metric, grouped.Threshold = "Group scoped", "cpu", 80
	grouped.Scope = model.AlertRuleScope{Kind: "groups", GroupIDs: []string{group.ID}}
	if err := validateAdvancedAlertRule(t.Context(), sqlite.DB, grouped); err != nil {
		t.Fatalf("group scoped rule = %v", err)
	}
	missing := grouped
	missing.Scope.GroupIDs = []string{"missing-group"}
	if err := validateAdvancedAlertRule(t.Context(), sqlite.DB, missing); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatalf("unknown group scope = %v", err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	if _, err := transaction.ExecContext(t.Context(), "DROP TABLE groups"); err != nil {
		t.Fatal(err)
	}
	if err := validateAdvancedAlertRule(t.Context(), transaction, grouped); err == nil {
		t.Fatal("rule scope check ignored a missing group table")
	}
}

func TestReplaceAlertRuleScopeReplacesRowsAndFailsClosed(t *testing.T) {
	sqlite, rules, mutation := evaluationRuleFixture(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "policy-scope-node", "192.0.2.50", 22)
	if err != nil {
		t.Fatal(err)
	}
	mutation.Rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{node.ID}}
	created, err := rules.Create(t.Context(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	duplicated := *created
	duplicated.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{node.ID, node.ID}}
	if err := replaceAlertRuleScope(t.Context(), sqlite.DB, duplicated); err == nil {
		t.Fatal("duplicate scope rows were accepted")
	}
	if err := replaceAlertRuleScope(t.Context(), covClosedHandle(t), duplicated); err == nil {
		t.Fatal("unreadable store accepted a scope replacement")
	}
	group, err := NewGroupStore(sqlite.DB).CreateGroup(t.Context(), "policy-scope-group")
	if err != nil {
		t.Fatal(err)
	}
	grouped := *created
	grouped.Scope = model.AlertRuleScope{Kind: "groups", GroupIDs: []string{group.ID}}
	if err := replaceAlertRuleScope(t.Context(), sqlite.DB, grouped); err != nil {
		t.Fatalf("group scope replacement: %v", err)
	}
	var nodes, groups int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM alert_rule_nodes WHERE rule_id=?", created.ID).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM alert_rule_groups WHERE rule_id=?", created.ID).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if nodes != 0 || groups != 1 {
		t.Fatalf("scope rows = nodes %d, groups %d", nodes, groups)
	}
}

func TestCheckAlertRuleCapacityRejectsRuleOverflow(t *testing.T) {
	sqlite := setupTestDB(t)
	if _, err := sqlite.DB.Exec(`WITH RECURSIVE seeds(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seeds WHERE n<=?)
		INSERT INTO alert_rules(id,name,metric,operator,threshold,created_at,updated_at)
		SELECT 'capacity-'||n,'Capacity '||n,'cpu','gt',80,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM seeds`, alerteval.MaximumRules); err != nil {
		t.Fatal(err)
	}
	if err := checkAlertRuleCapacity(t.Context(), sqlite.DB); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatalf("rule overflow = %v", err)
	}
}

func TestCheckAlertRuleCapacityFailsClosedWithoutScopeTables(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	if _, err := transaction.ExecContext(t.Context(), "DROP TABLE alert_rule_nodes"); err != nil {
		t.Fatal(err)
	}
	if err := checkAlertRuleCapacity(t.Context(), transaction); err == nil {
		t.Fatal("capacity check ignored a missing scope table")
	}
}

func TestGuardActiveAlertPolicyFailsClosedOnUnreadableStore(t *testing.T) {
	policy := activeAlertPolicy{
		previous: model.AlertEvaluationRule{AlertRule: model.AlertRule{ID: "rule", Metric: "cpu"}},
		next:     model.AlertEvaluationRule{AlertRule: model.AlertRule{ID: "rule", Metric: "memory"}},
	}
	if err := guardActiveAlertPolicy(t.Context(), covClosedHandle(t), policy); err == nil {
		t.Fatal("unreadable store allowed a numeric policy switch")
	}
}

func TestAuditAlertRuleRejectsUnknownSession(t *testing.T) {
	sqlite := setupTestDB(t)
	change := alertRuleAudit{rule: model.AlertEvaluationRule{AlertRule: model.AlertRule{ID: "rule"}},
		actor: EventMutationActor{UserID: "missing-user", SessionID: "missing-session"}, action: "update"}
	if err := auditAlertRule(t.Context(), sqlite.DB, change); err == nil {
		t.Fatal("audit with an unknown session succeeded")
	}
}
