package notification

import (
	"encoding/json"
	"strings"
)

func redactPreviewBody(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, ErrTemplate
	}
	budget := maximumJSONNodes
	redacted, err := redactPreviewValue(value, previewTraversal{budget: &budget})
	if err != nil {
		return nil, err
	}
	return json.Marshal(redacted)
}

type previewTraversal struct {
	budget *int
	depth  int
}

func redactPreviewValue(value any, traversal previewTraversal) (any, error) {
	*traversal.budget--
	if *traversal.budget < 0 || traversal.depth > maximumJSONDepth {
		return nil, ErrTemplate
	}
	traversal.depth++
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return redactTemplateLiterals(typed)
	case map[string]any:
		for name, item := range typed {
			redacted, err := redactPreviewValue(item, traversal)
			if err != nil {
				return nil, err
			}
			typed[name] = redacted
		}
		return typed, nil
	case []any:
		for index, item := range typed {
			redacted, err := redactPreviewValue(item, traversal)
			if err != nil {
				return nil, err
			}
			typed[index] = redacted
		}
		return typed, nil
	default:
		return "[REDACTED]", nil
	}
}

func redactTemplateLiterals(value string) (string, error) {
	parts, err := CompileTemplate(value, true)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, part := range parts {
		if part.Variable != "" {
			_, _ = output.WriteString("{{" + part.Variable + "}}")
		} else if strings.TrimSpace(part.Literal) == "" {
			_, _ = output.WriteString(part.Literal)
		} else {
			_, _ = output.WriteString("[REDACTED]")
		}
	}
	return output.String(), nil
}
