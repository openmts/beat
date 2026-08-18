package notification

import (
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// RenderTemplate replaces template placeholders with alert event values. When
// the template is empty a default message is returned. Supported placeholders:
// {{rule}}, {{node}}, {{metric}}, {{severity}}, {{value}}, {{status}},
// {{message}} and {{time}}.
func RenderTemplate(template string, event *model.AlertEvent) string {
	return RenderTemplateWith(template, event, nil)
}

// RenderTemplateWith behaves like RenderTemplate but applies escape to the
// dynamic placeholder values before substitution. escape is applied to the
// values that come from event data (rule, node, metric, severity, value,
// status, time and message), leaving the literal template markup intact.
func RenderTemplateWith(template string, event *model.AlertEvent, escape func(string) string) string {
	text := strings.TrimSpace(template)
	if text == "" {
		return deliveryText(event)
	}
	escapeIf := func(value string) string {
		if escape == nil {
			return value
		}
		return escape(value)
	}
	replacer := strings.NewReplacer(
		"{{rule}}", escapeIf(event.RuleName),
		"{{node}}", escapeIf(event.NodeName),
		"{{metric}}", escapeIf(event.Metric),
		"{{severity}}", escapeIf(string(event.Severity)),
		"{{value}}", escapeIf(fmt.Sprintf("%.2f", event.Value)),
		"{{status}}", escapeIf(string(event.Status)),
		"{{message}}", escapeIf(event.Message),
		"{{time}}", escapeIf(displayTime(event.TriggeredAt)),
	)
	return replacer.Replace(text)
}

// EscapeMarkdownV2 escapes Telegram MarkdownV2 reserved characters so dynamic
// values (which may contain dots, underscores, brackets, etc.) do not break
// message parsing.
func EscapeMarkdownV2(value string) string {
	const reserved = `\_*[]()~` + "`" + `>#+-=|{}.!`
	var builder strings.Builder
	for _, r := range value {
		if strings.ContainsRune(reserved, r) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func displayTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}
