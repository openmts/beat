package model

import (
	"errors"
	"strings"
	"testing"
)

func floatPointer(value float64) *float64 { return &value }

func mappingDefinition(kind string, mappings []DashboardValueMapping) DashboardDefinition {
	definition := DefaultMetricDashboard("admin").Definition
	definition.Panels[0].Type = kind
	definition.Panels[0].ValueMappings = mappings
	return definition
}

func TestDashboardValueMappingsAcceptDocumentedShapes(t *testing.T) {
	mappings := []DashboardValueMapping{
		{Type: "value", Value: floatPointer(0), Text: "offline", Color: "rose"},
		{Type: "range", From: floatPointer(1), To: floatPointer(60), Text: "warm"},
		{Type: "range", From: floatPointer(60), Color: "amber"},
		{Type: "range", To: floatPointer(0), Text: "below zero"},
		{Type: "special", Match: "null", Text: "no data"},
	}
	for _, kind := range []string{"stat", "gauge", "table"} {
		if err := ValidateDashboard(mappingDefinition(kind, mappings)); err != nil {
			t.Fatalf("%s panel rejected valid mappings: %v", kind, err)
		}
	}
}

func TestDashboardValueMappingsRejectInvalidShapes(t *testing.T) {
	cases := map[string][]DashboardValueMapping{
		"too many":         make([]DashboardValueMapping, MaxDashboardValueMappings+1),
		"unknown type":     {{Type: "regex", Text: "x"}},
		"empty mapping":    {{Type: "value", Value: floatPointer(1)}},
		"value missing":    {{Type: "value", Text: "x"}},
		"value with nan":   {{Type: "value", Value: floatPointer(0), Match: "null", Text: "x"}},
		"range missing":    {{Type: "range", Text: "x"}},
		"range reversed":   {{Type: "range", From: floatPointer(2), To: floatPointer(2), Text: "x"}},
		"range with match": {{Type: "range", From: floatPointer(0), Match: "null", Text: "x"}},
		"special match":    {{Type: "special", Match: "inf", Text: "x"}},
		"special value":    {{Type: "special", Match: "null", Value: floatPointer(1), Text: "x"}},
		"bad color":        {{Type: "value", Value: floatPointer(1), Color: "url(https://tracker)"}},
		"long text":        {{Type: "value", Value: floatPointer(1), Text: strings.Repeat("x", MaxDashboardMappingTextBytes+1)}},
		"control text":     {{Type: "value", Value: floatPointer(1), Text: "a\x00b"}},
		"timeseries":       {{Type: "value", Value: floatPointer(1), Text: "x"}},
	}
	for name, mappings := range cases {
		t.Run(name, func(t *testing.T) {
			kind := "stat"
			if name == "timeseries" {
				kind = "timeseries"
			}
			if err := ValidateDashboard(mappingDefinition(kind, mappings)); !errors.Is(err, ErrInvalidDashboard) {
				t.Fatalf("invalid value mappings accepted: %v", err)
			}
		})
	}
}
