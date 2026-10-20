package model

import (
	"errors"
	"strings"
	"testing"
)

func TestDashboardNoValueAcceptsSupportedPanels(t *testing.T) {
	for _, kind := range []string{"stat", "gauge", "bargauge", "table", "statetimeline", "piechart"} {
		definition := overrideDefinition(nil)
		definition.Panels[0].Type = kind
		definition.Panels[0].NoValue = "n/a"
		if kind == "stat" || kind == "gauge" {
			definition.Panels[0].Series = definition.Panels[0].Series[:1]
		}
		if err := ValidateDashboard(definition); err != nil {
			t.Fatalf("%s panel rejected the no value text: %v", kind, err)
		}
	}
	// 空值等于沿用渲染器默认的破折号，不属于配置。
	blank := overrideDefinition(nil)
	blank.Panels[0].NoValue = ""
	if err := ValidateDashboard(blank); err != nil {
		t.Fatalf("absent no value text must stay valid: %v", err)
	}
}

func TestDashboardNoValueRejectsUnusableText(t *testing.T) {
	definition := overrideDefinition(nil)
	definition.Panels[0].NoValue = "n/a"
	if err := ValidateDashboard(definition); !errors.Is(err, ErrInvalidDashboard) {
		t.Fatalf("time series panel accepted a no value text: %v", err)
	}
	for _, text := range []string{strings.Repeat("x", MaxPanelNoValueBytes+1), "two\nlines", " "} {
		panel := overrideDefinition(nil)
		panel.Panels[0].Type = "stat"
		panel.Panels[0].Series = panel.Panels[0].Series[:1]
		panel.Panels[0].NoValue = text
		if err := ValidateDashboard(panel); !errors.Is(err, ErrInvalidDashboard) {
			t.Fatalf("invalid no value text %q accepted: %v", text, err)
		}
	}
}
