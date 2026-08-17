package model

import (
	"encoding/json"
	"time"
)

const (
	AgentTaskCustomMetricRegister = "custom_metric_register"

	CustomMetricPrefix = "custom_"
)

// CustomMetric is the definition of a custom metric collection task.
type CustomMetric struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ShellCommand    string    `json:"shell_command"`
	IntervalSeconds int       `json:"interval_seconds"`
	Description     string    `json:"description"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CustomMetricRegisterPayload is the payload for a custom_metric_register
// agent task. It carries the full set of custom metrics bound to a node so the
// agent can replace its local registry.
type CustomMetricRegisterPayload struct {
	Metrics []CustomMetric `json:"metrics"`
}

// EncodeCustomMetricRegisterPayload builds a task payload.
func EncodeCustomMetricRegisterPayload(metrics []CustomMetric) (string, error) {
	payload, err := json.Marshal(CustomMetricRegisterPayload{Metrics: metrics})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// DecodeCustomMetricRegisterPayload extracts the metrics from a task payload.
func DecodeCustomMetricRegisterPayload(payload string) (*CustomMetricRegisterPayload, error) {
	var decoded CustomMetricRegisterPayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil, err
	}
	if decoded.Metrics == nil {
		return nil, ErrAgentTaskInvalidPayload
	}
	return &decoded, nil
}

// CustomMetricValue is a single collected custom metric sample.
type CustomMetricValue struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}
