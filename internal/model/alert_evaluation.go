package model

import "time"

const (
	AlertPhaseNormal     = "normal"
	AlertPhasePending    = "pending"
	AlertPhaseFiring     = "firing"
	AlertPhaseRecovering = "recovering"
	EvaluationOK         = "ok"
	EvaluationNoData     = "no_data"
	EvaluationError      = "error"
	EvaluationDisabled   = "disabled"
	EvaluationOutOfScope = "out_of_scope"
	EvaluationKeep       = "keep"
	EvaluationNotify     = "notify"
)

type AlertRuleScope struct {
	DeviceID string   `json:"device_id,omitempty"`
	Kind     string   `json:"kind"`
	NodeIDs  []string `json:"node_ids,omitempty"`
	GroupIDs []string `json:"group_ids,omitempty"`
}

type AlertEvaluationRule struct {
	AlertRule
	Revision            int64          `json:"revision"`
	NumericPolicyLocked bool           `json:"numeric_policy_locked"`
	DeletedAt           *time.Time     `json:"deleted_at,omitempty"`
	Scope               AlertRuleScope `json:"scope"`
	WindowSeconds       int            `json:"window_seconds"`
	Aggregation         string         `json:"aggregation"`
	RecoveryThreshold   *float64       `json:"recovery_threshold"`
	RecoveryDuration    int            `json:"recovery_duration"`
	NoDataPolicy        string         `json:"no_data_policy"`
	NoDataDelaySeconds  int            `json:"no_data_delay_seconds"`
	ErrorPolicy         string         `json:"error_policy"`
	ErrorDelaySeconds   int            `json:"error_delay_seconds"`
}

type AlertEvaluationEvent struct {
	DeviceID     string     `json:"device_id,omitempty"`
	DeviceName   string     `json:"device_name,omitempty"`
	ID           string     `json:"id"`
	RuleID       string     `json:"rule_id"`
	NodeID       string     `json:"node_id"`
	RuleRevision int64      `json:"rule_revision"`
	EpochID      string     `json:"epoch_id"`
	Kind         string     `json:"kind"`
	Reason       string     `json:"reason_code"`
	OccurredAt   time.Time  `json:"occurred_at"`
	NotifiedAt   *time.Time `json:"notified_at"`
	ClosedAt     *time.Time `json:"closed_at"`
	SourceKey    string     `json:"source_key"`
}

type AlertEvaluationObservation struct {
	ObservedAt time.Time
	RunID      string
	EvidenceID string
	SourceAt   time.Time
	Cadence    time.Duration
	Status     string
	Reason     string
	Value      *float64
}
