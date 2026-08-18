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
	text := strings.TrimSpace(template)
	if text == "" {
		return deliveryText(event)
	}
	replacer := strings.NewReplacer(
		"{{rule}}", event.RuleName,
		"{{node}}", event.NodeName,
		"{{metric}}", event.Metric,
		"{{severity}}", string(event.Severity),
		"{{value}}", fmt.Sprintf("%.2f", event.Value),
		"{{status}}", string(event.Status),
		"{{message}}", event.Message,
		"{{time}}", displayTime(event.TriggeredAt),
	)
	return replacer.Replace(text)
}

func displayTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}
