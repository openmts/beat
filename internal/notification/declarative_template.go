package notification

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrTemplate = errors.New("notification template is invalid")

type TemplatePart struct{ Literal, Variable string }

var messageVariables = map[string]bool{
	"event.kind": true, "event.status": true, "event.severity": true, "event.message": true, "event.occurred_at": true,
	"node.id": true, "node.name": true, "node.group_name": true, "node.labels": true, "rule.id": true, "rule.name": true,
	"metric.name": true, "metric.value": true, "metric.formatted_value": true, "site.title": true, "site.public_base_url": true,
	"security.authentication_method": true, "security.source_summary": true, "security.location_summary": true, "security.device_summary": true,
	"delivery.id": true, "delivery.attempt": true, "subject": true, "text": true,
	"evaluation.status": true, "evaluation.reason": true, "evaluation.epoch": true,
}

func CompileTemplate(template string, allowSecrets bool) ([]TemplatePart, error) {
	if !utf8.ValidString(template) || len(template) > 32*1024 || strings.ContainsRune(template, 0) {
		return nil, ErrTemplate
	}
	parts := []TemplatePart{}
	for template != "" {
		start := strings.Index(template, "{{")
		if start < 0 {
			if strings.Contains(template, "}}") {
				return nil, ErrTemplate
			}
			parts = append(parts, TemplatePart{Literal: template})
			break
		}
		if start > 0 {
			if strings.Contains(template[:start], "}}") {
				return nil, ErrTemplate
			}
			parts = append(parts, TemplatePart{Literal: template[:start]})
		}
		end := strings.Index(template[start+2:], "}}")
		if end < 0 {
			return nil, ErrTemplate
		}
		name := template[start+2 : start+2+end]
		if !validTemplateVariable(name, allowSecrets) {
			return nil, ErrTemplate
		}
		parts = append(parts, TemplatePart{Variable: name})
		template = template[start+2+end+2:]
	}
	return parts, nil
}

func validTemplateVariable(name string, allowSecrets bool) bool {
	return messageVariables[name] || allowSecrets && strings.HasPrefix(name, "secret.") && secretNamePattern.MatchString(strings.TrimPrefix(name, "secret."))
}

type TemplateValues struct {
	Data, Secrets map[string]string
	Maximum       int
	Escape        func(string) string
}

func RenderDeclarative(parts []TemplatePart, values TemplateValues) (string, error) {
	var output strings.Builder
	for _, part := range parts {
		value := part.Literal
		if part.Variable != "" {
			if strings.HasPrefix(part.Variable, "secret.") {
				var exists bool
				value, exists = values.Secrets[strings.TrimPrefix(part.Variable, "secret.")]
				if !exists || value == "" {
					return "", ErrTemplate
				}
			} else {
				value = values.Data[part.Variable]
			}
			if values.Escape != nil {
				value = values.Escape(value)
			}
		}
		if output.Len()+len(value) > values.Maximum {
			return "", ErrTemplate
		}
		_, _ = output.WriteString(value)
	}
	return output.String(), nil
}
