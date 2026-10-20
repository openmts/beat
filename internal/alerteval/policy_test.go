package alerteval

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func cpuPolicyDefinition() *model.MetricDefinition {
	low, high := 0.0, 100.0
	return &model.MetricDefinition{Key: "cpu", Enabled: true, Kind: "gauge", Aggregations: []string{"last", "avg", "min", "max"}, Minimum: &low, Maximum: &high}
}

func TestRulesAcceptBoundedExplicitScopesAndRejectAmbiguousTargets(t *testing.T) {
	for _, scope := range []model.AlertRuleScope{{Kind: "all"}, {Kind: "nodes", NodeIDs: []string{"node"}}, {Kind: "groups", GroupIDs: []string{"group"}}} {
		rule := stateRule()
		rule.Scope = scope
		if err := ValidateRule(rule, cpuPolicyDefinition()); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []model.AlertRuleScope{
		{Kind: "unknown"}, {Kind: "nodes"}, {Kind: "groups"}, {Kind: "all", NodeIDs: []string{"node"}},
		{Kind: "nodes", NodeIDs: []string{"a", "a"}}, {Kind: "groups", GroupIDs: []string{""}},
		{Kind: "nodes", NodeIDs: []string{"a"}, GroupIDs: []string{"b"}},
		{Kind: "groups", NodeIDs: []string{"a"}, GroupIDs: []string{"b"}},
		{Kind: "nodes", NodeIDs: make([]string, MaximumScopeNodes+1)},
		{Kind: "groups", GroupIDs: []string{"bad\nidentifier"}},
	} {
		rule := stateRule()
		rule.Scope = scope
		if err := ValidateRule(rule, cpuPolicyDefinition()); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("ambiguous scope accepted: %+v", scope)
		}
	}
}

func TestRulesValidateNumericDomainsWindowsAndNotificationPolicies(t *testing.T) {
	for name, change := range map[string]func(*model.AlertEvaluationRule){
		"empty name":          func(r *model.AlertEvaluationRule) { r.Name = " " },
		"long name":           func(r *model.AlertEvaluationRule) { r.Name = strings.Repeat("x", 129) },
		"control description": func(r *model.AlertEvaluationRule) { r.Description = "bad\x00description" },
		"invalid operator":    func(r *model.AlertEvaluationRule) { r.Operator = "bad" },
		"nan threshold":       func(r *model.AlertEvaluationRule) { r.Threshold = math.NaN() },
		"below domain":        func(r *model.AlertEvaluationRule) { r.Threshold = -1; r.RecoveryThreshold = nil },
		"above domain":        func(r *model.AlertEvaluationRule) { r.Threshold = 101 },
		"recovery direction":  func(r *model.AlertEvaluationRule) { v := 81.0; r.RecoveryThreshold = &v },
		"invalid recovery":    func(r *model.AlertEvaluationRule) { v := math.Inf(1); r.RecoveryThreshold = &v },
		"short window":        func(r *model.AlertEvaluationRule) { r.WindowSeconds = 1 },
		"long window":         func(r *model.AlertEvaluationRule) { r.WindowSeconds = MaximumWindowSeconds + 1 },
		"negative delay":      func(r *model.AlertEvaluationRule) { r.Duration = -1 },
		"long delay":          func(r *model.AlertEvaluationRule) { r.RecoveryDuration = MaximumDelaySeconds + 1 },
		"no data policy":      func(r *model.AlertEvaluationRule) { r.NoDataPolicy = "discard" },
		"error policy":        func(r *model.AlertEvaluationRule) { r.ErrorPolicy = "discard" },
		"severity":            func(r *model.AlertEvaluationRule) { r.Severity = "bad" },
		"current average":     func(r *model.AlertEvaluationRule) { r.Aggregation = "avg" },
		"unknown aggregation": func(r *model.AlertEvaluationRule) { r.Aggregation = "sum" },
	} {
		t.Run(name, func(t *testing.T) {
			r := stateRule()
			change(&r)
			if err := ValidateRule(r, cpuPolicyDefinition()); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("invalid rule accepted: %v", err)
			}
		})
	}
	rule := stateRule()
	rule.WindowSeconds = 60
	rule.Aggregation = "avg"
	if err := ValidateRule(rule, cpuPolicyDefinition()); err != nil {
		t.Fatal(err)
	}
	for _, definition := range []*model.MetricDefinition{nil, {Key: "other", Enabled: true}, {Key: "cpu", Enabled: false}} {
		if err := ValidateRule(rule, definition); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatal("ungoverned metric accepted")
		}
	}
}

func TestOperationalMetricPoliciesRemainCurrentOnly(t *testing.T) {
	for _, metric := range []string{model.MetricHeartbeatAgeSeconds, model.MetricTrafficUsagePercent} {
		rule := stateRule()
		rule.Metric = metric
		if err := ValidateRule(rule, nil); err != nil {
			t.Fatal(err)
		}
		rule.WindowSeconds = 60
		if err := ValidateRule(rule, nil); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatal("unsupported operational window accepted")
		}
	}
}

func TestComparisonAliasesAndEqualityBoundaries(t *testing.T) {
	for _, test := range []struct {
		operator, canonical string
		atEqual             bool
	}{
		{">", "gt", false}, {">=", "gte", true}, {"<", "lt", false}, {"<=", "lte", true}, {"==", "eq", true}, {"=", "eq", true}, {"!=", "ne", false},
	} {
		rule := stateRule()
		rule.Operator = test.operator
		rule.RecoveryThreshold = nil
		if CanonicalOperator(test.operator) != test.canonical || Triggered(80, rule) != test.atEqual || Recovered(80, rule) == test.atEqual {
			t.Fatalf("incorrect equality boundary for %s", test.operator)
		}
	}
	rule := stateRule()
	rule.Operator = "unknown"
	if CanonicalOperator(rule.Operator) != "" || Triggered(90, rule) || Recovered(70, rule) {
		t.Fatal("unknown comparator accepted")
	}
	for _, operator := range []string{"lt", "lte", "eq", "ne"} {
		rule := stateRule()
		rule.Operator = operator
		rule.Threshold = 20
		recovery := 25.0
		rule.RecoveryThreshold = &recovery
		if operator == "eq" || operator == "ne" {
			if err := ValidateRule(rule, cpuPolicyDefinition()); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatal("asymmetric equality recovery accepted")
			}
			recovery = 20
		}
		if err := ValidateRule(rule, cpuPolicyDefinition()); err != nil {
			t.Fatal(err)
		}
		if operator == "lt" || operator == "lte" {
			if Triggered(23, rule) || Recovered(23, rule) {
				t.Fatal("hysteresis hold band changed state")
			}
		}
	}
}
