package metriccatalog

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestBuiltinCatalogCoversNodeMetricsWithUnitsAndDomains(t *testing.T) {
	definitions := Builtins()
	seen := make(map[string]model.MetricDefinition)
	for _, definition := range definitions {
		if _, duplicate := seen[definition.Key]; duplicate {
			t.Fatal("duplicate builtin definition")
		}
		if err := ValidateDefinition(definition); err != nil {
			t.Fatalf("%s: %v", definition.Key, err)
		}
		seen[definition.Key] = definition
	}
	for _, key := range model.MetricNames() {
		if _, exists := seen[key]; !exists {
			t.Fatalf("missing builtin %s", key)
		}
	}
	if seen["cpu"].Unit != "percent" || seen["cpu"].Maximum == nil || *seen["cpu"].Maximum != 100 {
		t.Fatal("CPU domain changed")
	}
	if seen["memory_used"].Unit != "bytes" || seen["cpu_total"].Unit != "cores" {
		t.Fatal("capacity units changed")
	}
	if seen["net_recv_total"].Kind != "counter" || !slices.Contains(seen["net_recv_total"].Aggregations, "rate") {
		t.Fatal("counter rate is unavailable")
	}
	if seen["net_recv_delta"].Source != "derived" || seen["net_recv_delta"].DefaultAggregation != "sum" {
		t.Fatal("traffic delta aggregation changed")
	}
	definitions[0].Aggregations[0] = "bad"
	if err := ValidateDefinition(Builtins()[0]); err != nil {
		t.Fatal("catalog caller mutated seeds")
	}
}

func TestCustomCatalogIsBoundedAndDoesNotExposeInternalMetadata(t *testing.T) {
	definition, err := Custom("queue_depth", "Queue depth")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Key != model.CustomMetricPrefix+"queue_depth" || definition.Source != "custom" || definition.Unit != "number" {
		t.Fatal("custom identity changed")
	}
	if err := ValidateDefinition(definition); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tags", "minimum", "maximum", "created_at", "shell_command"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("internal field leaked: %s", field)
		}
	}
	for _, name := range []string{"", "bad/name", "bad name", strings.Repeat("a", 101)} {
		if _, err := Custom(name, ""); err == nil {
			t.Fatalf("invalid custom name %q accepted", name)
		}
	}
}

func TestCatalogValidationRejectsInvalidSemanticsAndPolicy(t *testing.T) {
	for name, mutate := range map[string]func(*model.MetricDefinition){
		"unknown source":        func(d *model.MetricDefinition) { d.Source = "unknown" },
		"unknown kind":          func(d *model.MetricDefinition) { d.Kind = "histogram" },
		"unknown unit":          func(d *model.MetricDefinition) { d.Unit = "unknown" },
		"empty label":           func(d *model.MetricDefinition) { d.Label = " " },
		"oversized description": func(d *model.MetricDefinition) { d.Description = strings.Repeat("x", MaximumDescriptionBytes+1) },
		"invalid retention":     func(d *model.MetricDefinition) { d.RetentionDays = MaximumRetentionDays + 1 },
		"invalid aggregation":   func(d *model.MetricDefinition) { d.DefaultAggregation = "rate" },
		"invalid domain":        func(d *model.MetricDefinition) { value := math.NaN(); d.Minimum = &value },
		"unbounded tag":         func(d *model.MetricDefinition) { d.Tags = []string{"anything"} },
	} {
		t.Run(name, func(t *testing.T) {
			definition := Builtins()[0]
			mutate(&definition)
			if err := ValidateDefinition(definition); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
}
