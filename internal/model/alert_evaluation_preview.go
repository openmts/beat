package model

import "time"

type AlertEvaluationPreviewRequest struct {
	Rule    AlertEvaluationRule `json:"rule"`
	NodeIDs []string            `json:"node_ids"`
}

type AlertEvaluationPreview struct {
	DeviceID   string     `json:"device_id,omitempty"`
	DeviceName string     `json:"device_name,omitempty"`
	NodeID     string     `json:"node_id"`
	Status     string     `json:"evaluation_status"`
	Reason     string     `json:"reason_code"`
	Value      *float64   `json:"value"`
	EvidenceID string     `json:"evidence_id,omitempty"`
	SourceAt   *time.Time `json:"source_at"`
}
