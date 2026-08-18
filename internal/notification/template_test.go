package notification

import (
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func templateTestEvent() *model.AlertEvent {
	ts := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	return &model.AlertEvent{
		RuleID: "rule-1", NodeID: "node-1",
		Message: "[critical] High CPU: cpu on node web-01, value=95.00 (threshold=90.00 gt)",
		Value:   95, Status: model.AlertStatusTriggered,
		TriggeredAt: ts,
		RuleName:    "High CPU", NodeName: "web-01", Metric: "cpu",
		Severity: model.AlertSeverityCritical,
	}
}

func TestRenderTemplateWithPlaceholders(t *testing.T) {
	event := templateTestEvent()
	template := "{{severity}} alert {{rule}} on {{node}}: {{metric}}={{value}} at {{time}} status={{status}}"
	rendered := RenderTemplate(template, event)
	if !strings.Contains(rendered, "critical alert High CPU on web-01: cpu=95.00") {
		t.Fatalf("rendered = %q", rendered)
	}
	if !strings.Contains(rendered, "2026-08-18T12:00:00Z") {
		t.Fatalf("rendered missing time: %q", rendered)
	}
	if !strings.Contains(rendered, "status=triggered") {
		t.Fatalf("rendered missing status: %q", rendered)
	}
}

func TestRenderTemplateFallsBackToDefault(t *testing.T) {
	event := templateTestEvent()
	rendered := RenderTemplate("", event)
	// The default template surfaces the formatted event message and timestamp.
	if !strings.Contains(rendered, event.Message) {
		t.Fatalf("default rendered = %q, want message %q", rendered, event.Message)
	}
	if !strings.Contains(rendered, "2026-08-18T12:00:00Z") {
		t.Fatalf("default rendered missing time: %q", rendered)
	}
}

func TestRenderTemplateReplacesUnknownVariablesAsLiteral(t *testing.T) {
	event := templateTestEvent()
	rendered := RenderTemplate("unknown {{foo}} here", event)
	if rendered != "unknown {{foo}} here" {
		t.Fatalf("rendered = %q", rendered)
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	got := EscapeMarkdownV2("node-a.example.com (web)")
	want := `node\-a\.example\.com \(web\)`
	if got != want {
		t.Fatalf("EscapeMarkdownV2 = %q, want %q", got, want)
	}
	if EscapeMarkdownV2("plain") != "plain" {
		t.Fatal("plain text should be unchanged")
	}
}

func TestRenderTemplateWithEscapesDynamicValues(t *testing.T) {
	event := templateTestEvent()
	// mock-rule value contains a dot to exercise escaping
	rich := *event
	rich.RuleName = "high.cpu"
	rich.NodeName = "web-01.example"
	rich.Message = "value=12.5"
	rendered := RenderTemplateWith("{{rule}} {{node}} {{value}} {{message}}", &rich, EscapeMarkdownV2)
	want := `high\.cpu web\-01\.example 95\.00 value\=12\.5`
	if rendered != want {
		t.Fatalf("rendered = %q, want %q", rendered, want)
	}
}

func TestRenderTemplateWithNilEscape(t *testing.T) {
	event := templateTestEvent()
	rendered := RenderTemplateWith("{{rule}}-{{metric}}", event, nil)
	if rendered != "High CPU-cpu" {
		t.Fatalf("rendered = %q", rendered)
	}
}
