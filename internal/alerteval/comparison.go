package alerteval

import "github.com/beat/backend/internal/model"

func CanonicalOperator(operator string) string {
	switch operator {
	case "gt", ">":
		return "gt"
	case "gte", ">=":
		return "gte"
	case "lt", "<":
		return "lt"
	case "lte", "<=":
		return "lte"
	case "eq", "==", "=":
		return "eq"
	case "ne", "!=":
		return "ne"
	default:
		return ""
	}
}

func Triggered(value float64, rule model.AlertEvaluationRule) bool {
	switch CanonicalOperator(rule.Operator) {
	case "gt":
		return value > rule.Threshold
	case "gte":
		return value >= rule.Threshold
	case "lt":
		return value < rule.Threshold
	case "lte":
		return value <= rule.Threshold
	case "eq":
		return value == rule.Threshold
	case "ne":
		return value != rule.Threshold
	default:
		return false
	}
}

func Recovered(value float64, rule model.AlertEvaluationRule) bool {
	threshold := rule.Threshold
	if rule.RecoveryThreshold != nil {
		threshold = *rule.RecoveryThreshold
	}
	switch CanonicalOperator(rule.Operator) {
	case "gt":
		return value <= threshold
	case "gte":
		return value < threshold
	case "lt":
		return value >= threshold
	case "lte":
		return value > threshold
	case "eq":
		return value != rule.Threshold
	case "ne":
		return value == rule.Threshold
	default:
		return false
	}
}
