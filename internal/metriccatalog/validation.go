package metriccatalog

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/beat/backend/internal/model"
)

func ValidateDefinition(definition model.MetricDefinition) error {
	if err := validateMetricIdentity(definition); err != nil {
		return err
	}
	if err := validateMetricPresentation(definition); err != nil {
		return err
	}
	if !validDefinitionDomain(definition) || !validMetricTags(definition) {
		return fmt.Errorf("%w: invalid value domain or tags", ErrInvalidDefinition)
	}
	return validateAggregations(definition)
}

func validateMetricIdentity(definition model.MetricDefinition) error {
	if !ValidKey(definition.Key) || !slices.Contains([]string{"builtin", "derived", "custom"}, definition.Source) {
		return fmt.Errorf("%w: unknown key or source", ErrInvalidDefinition)
	}
	if !slices.Contains([]string{"gauge", "counter"}, definition.Kind) || !validUnit(definition.Unit) {
		return fmt.Errorf("%w: unknown kind or unit", ErrInvalidDefinition)
	}
	return nil
}

func validateMetricPresentation(definition model.MetricDefinition) error {
	if !validCatalogText(definition.Label, MaximumLabelBytes) || strings.TrimSpace(definition.Label) == "" ||
		!validCatalogText(definition.Description, MaximumDescriptionBytes) {
		return fmt.Errorf("%w: label or description exceeds its bounds", ErrInvalidDefinition)
	}
	if definition.RetentionDays < MinimumRetentionDays || definition.RetentionDays > MaximumRetentionDays || definition.Revision < 1 {
		return fmt.Errorf("%w: invalid retention or revision", ErrInvalidDefinition)
	}
	return nil
}

func validUnit(unit string) bool {
	return slices.Contains([]string{"percent", "bytes", "bytes_per_second", "seconds", "milliseconds", "celsius", "watts", "count", "cores", "number"}, unit)
}

func validCatalogText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validDefinitionDomain(definition model.MetricDefinition) bool {
	for _, bound := range []*float64{definition.Minimum, definition.Maximum} {
		if bound != nil && (math.IsNaN(*bound) || math.IsInf(*bound, 0)) {
			return false
		}
	}
	return definition.Minimum == nil || definition.Maximum == nil || *definition.Minimum <= *definition.Maximum
}

func validateAggregations(definition model.MetricDefinition) error {
	if len(definition.Aggregations) == 0 || len(definition.Aggregations) > 6 || !slices.Contains(definition.Aggregations, definition.DefaultAggregation) {
		return fmt.Errorf("%w: invalid default aggregation", ErrInvalidDefinition)
	}
	seen := make(map[string]bool)
	for _, aggregation := range definition.Aggregations {
		if seen[aggregation] || !slices.Contains([]string{"avg", "min", "max", "sum", "last", "rate"}, aggregation) {
			return fmt.Errorf("%w: invalid aggregation", ErrInvalidDefinition)
		}
		if aggregation == "rate" && definition.Kind != "counter" {
			return fmt.Errorf("%w: rate requires a counter", ErrInvalidDefinition)
		}
		seen[aggregation] = true
	}
	return nil
}

func SameSemantics(first, second model.MetricDefinition) bool {
	return first.Key == second.Key && first.Source == second.Source && first.Kind == second.Kind && first.Unit == second.Unit &&
		slices.Equal(first.Aggregations, second.Aggregations) && slices.Equal(first.Tags, second.Tags) &&
		sameBound(first.Minimum, second.Minimum) && sameBound(first.Maximum, second.Maximum)
}

func sameBound(first, second *float64) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

func ApplyPolicy(definition model.MetricDefinition, policy model.MetricPolicyUpdate) (model.MetricDefinition, error) {
	definition.Label, definition.Description = policy.Label, policy.Description
	definition.DefaultAggregation = policy.DefaultAggregation
	definition.Public, definition.Enabled = policy.Public, policy.Enabled
	definition.RetentionOverride = policy.RetentionDays
	definition.RetentionDays = DefaultRetentionDays
	if policy.RetentionDays != nil {
		definition.RetentionDays = *policy.RetentionDays
	}
	if policy.Revision < 1 {
		return model.MetricDefinition{}, fmt.Errorf("%w: revision is required", ErrInvalidDefinition)
	}
	if err := ValidateDefinition(definition); err != nil {
		return model.MetricDefinition{}, err
	}
	return definition, nil
}
