package taskmetrics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var keyValueLinePattern = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*[=:]\s*("?[^"\r\n]*"?)\s*$`)

func Extract(cfg Config, output string) (map[string]float64, error) {
	if !cfg.MetricSchema.Enabled || len(cfg.MetricSchema.Metrics) == 0 {
		return map[string]float64{}, nil
	}
	values := make(map[string]float64, len(cfg.MetricSchema.Metrics))
	var document any
	documentLoaded := false
	var keyValues map[string]string
	keyValuesLoaded := false

	for _, metric := range cfg.MetricSchema.Metrics {
		var value float64
		var found bool
		switch metric.Extractor.Type {
		case "json_path":
			if !documentLoaded {
				decoder := json.NewDecoder(bytes.NewBufferString(strings.TrimSpace(output)))
				decoder.UseNumber()
				if err := decoder.Decode(&document); err != nil {
					document = nil
				}
				documentLoaded = true
			}
			if document != nil {
				var candidate any
				candidate, found = lookupJSONPath(document, metric.Extractor.Expression)
				if found {
					value, found = numericValue(candidate)
				}
			}
		case "key_value":
			if !keyValuesLoaded {
				keyValues = parseKeyValueLines(output)
				keyValuesLoaded = true
			}
			raw, ok := keyValues[metric.Extractor.Expression]
			if ok {
				value, found = numericString(raw)
			}
		case "regex":
			expression, err := regexp.Compile(metric.Extractor.Expression)
			if err != nil {
				return nil, fmt.Errorf("compile metric %s regex: %w", metric.Key, err)
			}
			match := expression.FindStringSubmatch(output)
			if len(match) > 0 {
				raw := match[0]
				if len(match) > 1 {
					raw = match[1]
				}
				value, found = numericString(raw)
			}
		}
		if found {
			values[metric.Key] = value
		}
	}
	return values, nil
}

func parseKeyValueLines(output string) map[string]string {
	values := make(map[string]string)
	for _, match := range keyValueLinePattern.FindAllStringSubmatch(output, -1) {
		key := strings.TrimSpace(match[1])
		value := strings.Trim(strings.TrimSpace(match[2]), `"'`)
		if key != "" {
			values[key] = value
		}
	}
	return values
}

func lookupJSONPath(document any, path string) (any, bool) {
	tokens, err := parseJSONPath(path)
	if err != nil {
		return nil, false
	}
	current := document
	for _, token := range tokens {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func parseJSONPath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "$") {
		return nil, fmt.Errorf("JSON path must start with $")
	}
	tokens := make([]string, 0, 4)
	for index := 1; index < len(path); {
		switch path[index] {
		case '.':
			index++
		case '[':
			end := strings.IndexByte(path[index:], ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed JSON path bracket")
			}
			token := strings.TrimSpace(path[index+1 : index+end])
			token = strings.Trim(token, `"'`)
			if token == "" {
				return nil, fmt.Errorf("empty JSON path token")
			}
			tokens = append(tokens, token)
			index += end + 1
		default:
			end := index
			for end < len(path) && path[end] != '.' && path[end] != '[' {
				end++
			}
			token := strings.TrimSpace(path[index:end])
			if token == "" {
				return nil, fmt.Errorf("empty JSON path token")
			}
			tokens = append(tokens, token)
			index = end
		}
	}
	return tokens, nil
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		return numericString(typed)
	default:
		return 0, false
	}
}

func numericString(value string) (float64, bool) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
