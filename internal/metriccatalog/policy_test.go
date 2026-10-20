package metriccatalog

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMetricPolicyPreservesSemanticsAndExplicitInheritance(t *testing.T) {
	definition := Builtins()[0]
	policy := model.MetricPolicyUpdate{Revision: 1, Label: "CPU label", Description: "Load", DefaultAggregation: "max", Public: false, Enabled: true}
	changed, err := ApplyPolicy(definition, policy)
	if err != nil || changed.Label != policy.Label || changed.RetentionOverride != nil || !SameSemantics(definition, changed) {
		t.Fatalf("policy changed machine semantics: %v", err)
	}
	days := 7
	policy.RetentionDays = &days
	changed, err = ApplyPolicy(definition, policy)
	if err != nil || changed.RetentionDays != 7 || changed.RetentionOverride == nil || *changed.RetentionOverride != 7 {
		t.Fatal("explicit retention was lost")
	}
	policy.Revision = 0
	if _, err := ApplyPolicy(definition, policy); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatal("revisionless policy accepted")
	}
	policy.Revision = 1
	days = 0
	if _, err := ApplyPolicy(definition, policy); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatal("destructive zero retention accepted")
	}
}

func TestCatalogSemanticChangesAndInvalidAggregationDomainsAreRejected(t *testing.T) {
	for name, change := range map[string]func(*model.MetricDefinition){
		"empty aggregations":    func(d *model.MetricDefinition) { d.Aggregations = nil },
		"duplicate aggregation": func(d *model.MetricDefinition) { d.Aggregations = []string{"avg", "avg"} },
		"unknown aggregation":   func(d *model.MetricDefinition) { d.Aggregations = []string{"avg", "bad"} },
		"gauge rate":            func(d *model.MetricDefinition) { d.Aggregations = []string{"avg", "rate"} },
		"negative domain":       func(d *model.MetricDefinition) { d.Minimum = numberBound(101) },
		"infinite maximum":      func(d *model.MetricDefinition) { d.Maximum = numberBound(math.Inf(1)) },
		"invalid UTF8":          func(d *model.MetricDefinition) { d.Label = string([]byte{255}) },
		"NUL text":              func(d *model.MetricDefinition) { d.Description = "bad\x00text" },
	} {
		t.Run(name, func(t *testing.T) {
			definition := Builtins()[0]
			change(&definition)
			if err := ValidateDefinition(definition); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatal("invalid catalog metadata accepted")
			}
		})
	}
	first := Builtins()[0]
	second := first
	second.Maximum = nil
	if SameSemantics(first, second) {
		t.Fatal("value bound was mutable")
	}
	second = first
	second.Maximum = numberBound(99)
	if SameSemantics(first, second) {
		t.Fatal("numeric domain changed invisibly")
	}
	custom, err := Custom("load", "")
	if err != nil {
		t.Fatal(err)
	}
	if !SameSemantics(custom, custom) {
		t.Fatal("unbounded custom domain is unstable")
	}
	if _, err := Custom("valid", strings.Repeat("x", MaximumDescriptionBytes+1)); err == nil {
		t.Fatal("oversized custom description accepted")
	}
}
