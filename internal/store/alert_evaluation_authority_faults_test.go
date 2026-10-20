package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

// covAcceleratorDevice 是 32 位十六进制的合法加速卡设备标识。
const covAcceleratorDevice = "0123456789abcdef0123456789abcdef"

// covStaleRevision 是一个不可能等于目录定义真实修订号的探测值。
const covStaleRevision = 999

func covEvaluationRefs(keys ...string) []AlertMetricPolicyReference {
	references := make([]AlertMetricPolicyReference, 0, len(keys))
	for _, key := range keys {
		references = append(references, AlertMetricPolicyReference{Key: key, Revision: 1})
	}
	return references
}

func covScopeRule(id, metric, kind, deviceID string, ids ...string) *model.AlertEvaluationRule {
	rule := &model.AlertEvaluationRule{AlertRule: model.AlertRule{ID: id, Metric: metric}, Scope: model.AlertRuleScope{Kind: kind, DeviceID: deviceID}}
	if kind == "groups" {
		rule.Scope.GroupIDs = ids
	} else {
		rule.Scope.NodeIDs = ids
	}
	return rule
}

func TestEvaluationScopeMatchesHonoursDeviceAndScopeKind(t *testing.T) {
	sqlite, rules, mutation := evaluationRuleFixture(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "scope-node", "192.0.2.40", 22)
	if err != nil {
		t.Fatal(err)
	}
	mutation.Rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{node.ID}}
	created, err := rules.Create(t.Context(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	accelerator := covScopeRule(created.ID, model.MetricGPUMemoryPercent, "nodes", model.AcceleratorAnyDevice, node.ID)
	cases := []struct {
		name      string
		rule      *model.AlertEvaluationRule
		nodeID    string
		deviceID  string
		wantMatch bool
	}{
		{"accelerator device id malformed", accelerator, node.ID, "not-a-device", false},
		{"accelerator device inside rule scope", accelerator, node.ID, covAcceleratorDevice, true},
		{"scalar metric rejects accelerator device", created, node.ID, covAcceleratorDevice, false},
		{"node scope member", created, node.ID, "", true},
		{"node scope stranger", created, "node-outside-scope", "", false},
		{"all scope unknown node", covScopeRule(created.ID, "cpu", "all", ""), "node-outside-scope", "", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			matched, err := evaluationScopeMatches(t.Context(), transaction, evaluationScopeSelection{rule: test.rule, nodeID: test.nodeID, deviceID: test.deviceID})
			if err != nil {
				t.Fatalf("scope match: %v", err)
			}
			if matched != test.wantMatch {
				t.Fatalf("matched = %v, want %v", matched, test.wantMatch)
			}
		})
	}
}

func TestEvaluationScopeMatchesRejectsForeignAcceleratorDevice(t *testing.T) {
	sqlite := setupTestDB(t)
	transaction := alertTestTx(t, sqlite.DB)
	rule := covScopeRule("device-scoped-rule", model.MetricGPUMemoryPercent, "all", "fedcba9876543210fedcba9876543210")
	matched, err := evaluationScopeMatches(t.Context(), transaction, evaluationScopeSelection{rule: rule, nodeID: "device-scoped-node", deviceID: covAcceleratorDevice})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Fatal("evaluation matched a device outside the rule's accelerator scope")
	}
	malformed := covScopeRule("device-scoped-rule", model.MetricGPUMemoryPercent, "all", model.AcceleratorAnyDevice)
	matched, err = evaluationScopeMatches(t.Context(), transaction, evaluationScopeSelection{rule: malformed, nodeID: "device-scoped-node", deviceID: "not-a-device-id"})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Fatal("evaluation matched a malformed accelerator device identity")
	}
}

func TestEvaluationScopeMatchesResolvesGroupMembership(t *testing.T) {
	sqlite, rules, mutation := evaluationRuleFixture(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "scope-group-node", "192.0.2.42", 22)
	if err != nil {
		t.Fatal(err)
	}
	mutation.Rule.Scope = model.AlertRuleScope{Kind: "groups", GroupIDs: []string{node.GroupID}}
	created, err := rules.Create(t.Context(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := NewGroupStore(sqlite.DB).CreateGroup(t.Context(), "scope-outside")
	if err != nil {
		t.Fatal(err)
	}
	transaction := alertTestTx(t, sqlite.DB)
	rule := covScopeRule(created.ID, "cpu", "groups", "", node.GroupID)
	matched, err := evaluationScopeMatches(t.Context(), transaction, evaluationScopeSelection{rule: rule, nodeID: node.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("group scope did not match a member node")
	}
	if _, err := transaction.ExecContext(t.Context(), "DELETE FROM alert_rule_groups WHERE rule_id=?", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), "INSERT INTO alert_rule_groups(rule_id,group_id) VALUES(?,?)", created.ID, outsider.ID); err != nil {
		t.Fatal(err)
	}
	matched, err = evaluationScopeMatches(t.Context(), transaction, evaluationScopeSelection{rule: rule, nodeID: node.ID})
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		t.Fatal("group scope matched a node from another group")
	}
}

func TestExpectedEvaluationPoliciesRequiresExactlyOneRevisionedReference(t *testing.T) {
	heartbeat := model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: model.MetricHeartbeatAgeSeconds}}
	if !expectedEvaluationPolicies(heartbeat, nil) {
		t.Fatal("heartbeat rule must not require metric policies")
	}
	traffic := model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: model.MetricTrafficUsagePercent}}
	if !expectedEvaluationPolicies(traffic, covEvaluationRefs("net_recv_delta", "net_sent_delta")) {
		t.Fatal("traffic rule must accept its two derived inputs")
	}
	if expectedEvaluationPolicies(traffic, covEvaluationRefs("net_recv_delta")) {
		t.Fatal("traffic rule accepted a single input")
	}
	if expectedEvaluationPolicies(traffic, covEvaluationRefs("net_recv_delta", "net_recv_delta")) {
		t.Fatal("traffic rule accepted a duplicated input")
	}
	if expectedEvaluationPolicies(heartbeat, covEvaluationRefs("cpu")) {
		t.Fatal("rule without derived inputs accepted a policy reference")
	}
	if !expectedEvaluationPolicies(model.AlertEvaluationRule{AlertRule: model.AlertRule{Metric: "cpu"}}, covEvaluationRefs("cpu")) {
		t.Fatal("scalar rule rejected its own catalog reference")
	}
	unrevisioned := []AlertMetricPolicyReference{{Key: "net_recv_delta"}, {Key: "net_sent_delta"}}
	if expectedEvaluationPolicies(traffic, unrevisioned) {
		t.Fatal("policy reference without a revision was accepted")
	}
}
