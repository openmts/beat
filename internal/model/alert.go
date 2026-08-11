package model

import "time"

const (
	MetricTrafficUsagePercent = "traffic_usage_percent"
	MetricHeartbeatAgeSeconds = "heartbeat_age_seconds"
)

type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

type AlertStatus string

const (
	AlertStatusTriggered AlertStatus = "triggered"
	AlertStatusResolved  AlertStatus = "resolved"
)

type AlertRule struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Metric      string        `json:"metric"`
	Operator    string        `json:"operator"`
	Threshold   float64       `json:"threshold"`
	Duration    int           `json:"duration"`
	Severity    AlertSeverity `json:"severity"`
	Enabled     bool          `json:"enabled"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type AlertChannel struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	ChannelType  string               `json:"channel_type"`
	Config       string               `json:"config"`
	Enabled      bool                 `json:"enabled"`
	LastDelivery *AlertDeliveryStatus `json:"last_delivery,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

type AlertDeliveryStatus struct {
	State       string    `json:"state"`
	Message     string    `json:"message"`
	DeliveredAt time.Time `json:"delivered_at"`
}

type AlertEvent struct {
	ID          string      `json:"id"`
	RuleID      string      `json:"rule_id"`
	NodeID      string      `json:"node_id"`
	Message     string      `json:"message"`
	Value       float64     `json:"value"`
	Status      AlertStatus `json:"status"`
	TriggeredAt time.Time   `json:"triggered_at"`
	ResolvedAt  *time.Time  `json:"resolved_at"`
}
