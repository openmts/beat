package model

import (
	"errors"
	"strings"
	"testing"
)

func TestDashboardTagsAcceptBoundedUniqueLabels(t *testing.T) {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Tags = []string{"prod", "hk-1", "night_shift", "a0"}
	if err := ValidateDashboard(definition); err != nil {
		t.Fatalf("valid dashboard tags rejected: %v", err)
	}
	if len(definition.Tags) > MaxDashboardTags {
		t.Fatal("fixture exceeds the documented tag budget")
	}
}

func TestDashboardTagsRejectInvalidValues(t *testing.T) {
	cases := map[string][]string{
		"too many":     {"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9"},
		"empty":        {""},
		"uppercase":    {"Prod"},
		"space":        {"night shift"},
		"slash":        {"prod/hk"},
		"leading dash": {"-prod"},
		"too long":     {strings.Repeat("a", MaxAnnotationTagBytes+1)},
		"duplicate":    {"prod", "prod"},
	}
	for name, tags := range cases {
		t.Run(name, func(t *testing.T) {
			definition := DefaultMetricDashboard("public").Definition
			definition.Tags = tags
			if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid tags accepted: %v", err)
			}
		})
	}
}
