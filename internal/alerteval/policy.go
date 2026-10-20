package alerteval

import (
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

func DefaultRule() model.AlertEvaluationRule {
	return model.AlertEvaluationRule{AlertRule: model.AlertRule{Operator: "gt", Severity: model.AlertSeverityWarning, Enabled: true}, Revision: 1,
		Scope: model.AlertRuleScope{Kind: "all"}, Aggregation: "last", NoDataPolicy: model.EvaluationKeep, ErrorPolicy: model.EvaluationKeep,
		NoDataDelaySeconds: DefaultNoDataDelaySeconds, ErrorDelaySeconds: DefaultErrorDelaySeconds}
}

func ValidateRule(rule model.AlertEvaluationRule, definition *model.MetricDefinition) error {
	if err := validateAcceleratorScope(rule); err != nil {
		return err
	}
	if definition == nil {
		definition = model.AcceleratorAlertDefinition(rule.Metric)
	}
	if !validText(rule.Name, 128) || strings.TrimSpace(rule.Name) == "" || !validText(rule.Description, 2048) {
		return ErrInvalidPolicy
	}
	if !validScope(rule.Scope) || !validComparison(rule) || !validDelays(rule) {
		return ErrInvalidPolicy
	}
	if !slices.Contains([]model.AlertSeverity{model.AlertSeverityCritical, model.AlertSeverityWarning, model.AlertSeverityInfo}, rule.Severity) {
		return ErrInvalidPolicy
	}
	if rule.Metric == model.MetricHeartbeatAgeSeconds || rule.Metric == model.MetricTrafficUsagePercent {
		if rule.WindowSeconds != 0 || rule.Aggregation != "last" || rule.Threshold < 0 {
			return ErrInvalidPolicy
		}
		return nil
	}
	if definition == nil || definition.Key != rule.Metric || !definition.Enabled || !slices.Contains(definition.Aggregations, rule.Aggregation) {
		return ErrInvalidPolicy
	}
	if rule.WindowSeconds == 0 && rule.Aggregation != "last" {
		return ErrInvalidPolicy
	}
	for _, threshold := range numericThresholds(rule) {
		if definition.Minimum != nil && threshold < *definition.Minimum || definition.Maximum != nil && threshold > *definition.Maximum {
			return ErrInvalidPolicy
		}
	}
	return nil
}

func validComparison(rule model.AlertEvaluationRule) bool {
	op := CanonicalOperator(rule.Operator)
	if op == "" || !finite(rule.Threshold) {
		return false
	}
	if rule.RecoveryThreshold == nil {
		return true
	}
	recovery := *rule.RecoveryThreshold
	if !finite(recovery) {
		return false
	}
	switch op {
	case "gt", "gte":
		return recovery <= rule.Threshold
	case "lt", "lte":
		return recovery >= rule.Threshold
	default:
		return recovery == rule.Threshold
	}
}

func validDelays(rule model.AlertEvaluationRule) bool {
	if rule.WindowSeconds != 0 && (rule.WindowSeconds < MinimumWindowSeconds || rule.WindowSeconds > MaximumWindowSeconds) {
		return false
	}
	for _, delay := range []int{rule.Duration, rule.RecoveryDuration, rule.NoDataDelaySeconds, rule.ErrorDelaySeconds} {
		if delay < 0 || delay > MaximumDelaySeconds {
			return false
		}
	}
	for _, policy := range []string{rule.NoDataPolicy, rule.ErrorPolicy} {
		if policy != model.EvaluationKeep && policy != model.EvaluationNotify {
			return false
		}
	}
	return true
}

func validScope(scope model.AlertRuleScope) bool {
	switch scope.Kind {
	case "all":
		return len(scope.NodeIDs) == 0 && len(scope.GroupIDs) == 0
	case "nodes":
		return len(scope.GroupIDs) == 0 && validTargets(scope.NodeIDs, MaximumScopeNodes)
	case "groups":
		return len(scope.NodeIDs) == 0 && validTargets(scope.GroupIDs, MaximumScopeGroups)
	default:
		return false
	}
}

func validTargets(ids []string, maximum int) bool {
	if len(ids) == 0 || len(ids) > maximum {
		return false
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || !validText(id, 128) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func numericThresholds(rule model.AlertEvaluationRule) []float64 {
	values := []float64{rule.Threshold}
	if rule.RecoveryThreshold != nil {
		values = append(values, *rule.RecoveryThreshold)
	}
	return values
}

func validText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validateAcceleratorScope(rule model.AlertEvaluationRule) error {
	if !model.IsAcceleratorAlertMetric(rule.Metric) {
		if rule.Scope.DeviceID != "" {
			return ErrInvalidPolicy
		}
		return nil
	}
	if rule.Scope.Kind != "nodes" || len(rule.Scope.NodeIDs) != 1 || (rule.Scope.DeviceID != model.AcceleratorAnyDevice && !model.ValidAcceleratorDeviceID(rule.Scope.DeviceID)) {
		return ErrInvalidPolicy
	}
	return nil
}
