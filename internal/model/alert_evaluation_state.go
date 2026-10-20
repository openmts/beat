package model

import "time"

type AlertEvaluationState struct {
	DeviceID               string                `json:"device_id,omitempty"`
	DeviceName             string                `json:"device_name,omitempty"`
	RuleID                 string                `json:"rule_id"`
	NodeID                 string                `json:"node_id"`
	RuleRevision           int64                 `json:"rule_revision"`
	Version                int64                 `json:"state_version"`
	Phase                  string                `json:"phase"`
	Status                 string                `json:"evaluation_status"`
	Reason                 string                `json:"reason_code"`
	TriggerElapsedMS       int64                 `json:"trigger_elapsed_ms"`
	RecoveryElapsedMS      int64                 `json:"recovery_elapsed_ms"`
	PendingSince           *time.Time            `json:"pending_since"`
	RecoverySince          *time.Time            `json:"recovery_since"`
	LastEvaluatedAt        *time.Time            `json:"last_evaluated_at"`
	LastGoodAt             *time.Time            `json:"last_good_at"`
	LastEvidenceAt         *time.Time            `json:"last_evidence_at"`
	LastEvidenceObservedAt *time.Time            `json:"last_evidence_observed_at"`
	EvidenceID             string                `json:"evidence_id"`
	RunID                  string                `json:"-"`
	ActiveEventID          string                `json:"active_event_id"`
	ActiveEventStatus      AlertStatus           `json:"active_event_status"`
	Health                 AlertEvaluationHealth `json:"health"`
	NextDueAt              time.Time             `json:"next_due_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
}

type AlertEvaluationHealth struct {
	EpochID        string     `json:"epoch_id"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"started_at"`
	StatusSince    *time.Time `json:"status_since"`
	LastObservedAt *time.Time `json:"last_observed_at"`
	ElapsedMS      int64      `json:"elapsed_ms"`
	SeenMask       int        `json:"-"`
	NotifiedMask   int        `json:"-"`
	Suspended      bool       `json:"suspended"`
}
