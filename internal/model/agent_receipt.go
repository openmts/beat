package model

import "time"

const AgentReceiptMeasurement = "agent_ingest_receipt"

// AgentReceipt 仅包含回执身份和策略；数值指标独立保存在同一 MTS 批次。
type AgentReceipt struct {
	SampleID              string
	CapturedAt            time.Time
	PayloadDigest         string
	CredentialFingerprint string
	ProtocolVersion       int
	ReportIntervalMS      int64
	CustomSamplesJSON     string
}

type AgentReportACK struct {
	AcceleratorPolicy *AcceleratorPolicy `json:"accelerator_policy,omitempty"`
	ProtocolVersion   int                `json:"protocol_version"`
	SampleID          string             `json:"sample_id"`
	CapturedAt        time.Time          `json:"captured_at"`
	ContactAccepted   bool               `json:"contact_accepted"`
	MetricsAccepted   bool               `json:"metrics_accepted"`
	ContactAt         *time.Time         `json:"contact_at"`
	SSHPublicKey      string             `json:"ssh_public_key,omitempty"`
	ErrorCode         string             `json:"error_code,omitempty"`
	Retryable         bool               `json:"retryable"`
	Duplicate         bool               `json:"duplicate,omitempty"`
}

func (ack AgentReportACK) Matches(report AgentReport) bool {
	return ack.ProtocolVersion == AgentReportProtocol && ack.SampleID == report.SampleID &&
		ack.CapturedAt.Equal(report.CapturedAt)
}
