package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	AgentReportProtocol         = 2
	MaximumAgentClockSkew       = 2 * time.Minute
	MaximumAgentSampleAge       = 15 * time.Minute
	MaximumAgentReportBytes     = 128 << 10
	MaximumAgentHostBytes       = 255
	MaximumAgentNameBytes       = 128
	MaximumAgentInventoryBytes  = 512
	MaximumAgentCustomMetrics   = 64
	MaximumAgentCustomNameBytes = 100
	MinimumAgentReportInterval  = time.Second
	MaximumAgentReportInterval  = 24 * time.Hour
)

var (
	ErrAgentReportProtocol = errors.New("unsupported agent report protocol")
	ErrAgentReportInvalid  = errors.New("invalid agent report")
	ErrAgentReportFuture   = errors.New("agent sample clock is too far ahead")
	ErrAgentReportExpired  = errors.New("agent sample retry window expired")
)

// AgentReport 的内容在采集完成后固定；重试不得替换时间、计数器或自定义值。
type AgentReport struct {
	Accelerators     *AcceleratorReport              `json:"accelerators,omitempty"`
	ProtocolVersion  int                             `json:"protocol_version"`
	SampleID         string                          `json:"sample_id"`
	CapturedAt       time.Time                       `json:"captured_at"`
	ReportIntervalMS int64                           `json:"report_interval_ms"`
	Name             string                          `json:"name"`
	Host             string                          `json:"host"`
	Port             int                             `json:"port"`
	System           SystemInfo                      `json:"system"`
	Metrics          *NodeMetrics                    `json:"metrics"`
	CustomMetrics    map[string]float64              `json:"custom_metrics,omitempty"`
	CustomSamples    map[string]CustomSampleMetadata `json:"custom_samples,omitempty"`
}

func (report AgentReport) Validate(now time.Time) error {
	if report.ProtocolVersion != AgentReportProtocol {
		return ErrAgentReportProtocol
	}
	if err := report.validateTime(now); err != nil {
		return err
	}
	id, err := uuid.Parse(report.SampleID)
	if err != nil || id == uuid.Nil || id.String() != report.SampleID {
		return ErrAgentReportInvalid
	}
	if !report.validMetadata() || report.Metrics == nil {
		return ErrAgentReportInvalid
	}
	if err := report.Metrics.Validate(); err != nil {
		return ErrAgentReportInvalid
	}
	if err := validateAgentCustomValues(report.CustomMetrics); err != nil {
		return err
	}
	if err := ValidateCustomSamples(report.CustomMetrics, report.CustomSamples, report.CapturedAt); err != nil {
		return err
	}
	if report.Accelerators != nil {
		return report.Accelerators.Validate(report.CapturedAt)
	}
	return nil
}

func (report AgentReport) validateTime(now time.Time) error {
	if report.CapturedAt.IsZero() || report.CapturedAt.Unix() <= 0 {
		return ErrAgentReportInvalid
	}
	if report.CapturedAt.After(now.Add(MaximumAgentClockSkew)) {
		return ErrAgentReportFuture
	}
	if report.CapturedAt.Before(now.Add(-MaximumAgentSampleAge)) {
		return ErrAgentReportExpired
	}
	return nil
}

func (report AgentReport) validMetadata() bool {
	if report.ReportIntervalMS < MinimumAgentReportInterval.Milliseconds() || report.ReportIntervalMS > MaximumAgentReportInterval.Milliseconds() {
		return false
	}
	if report.Port < 1 || report.Port > 65535 || !validAgentText(report.Host, MaximumAgentHostBytes) || strings.TrimSpace(report.Host) == "" {
		return false
	}
	if !validAgentText(report.Name, MaximumAgentNameBytes) {
		return false
	}
	for _, value := range []string{report.System.CPUModel, report.System.OS, report.System.Platform, report.System.OSVersion,
		report.System.Kernel, report.System.Arch, report.System.Virtualization, report.System.AgentVersion} {
		if !validAgentText(value, MaximumAgentInventoryBytes) {
			return false
		}
	}
	return true
}

func validAgentText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func validateAgentCustomValues(values map[string]float64) error {
	if len(values) > MaximumAgentCustomMetrics {
		return ErrAgentReportInvalid
	}
	for name, value := range values {
		if !validAgentCustomName(name) || !isFinite(value) {
			return ErrAgentReportInvalid
		}
	}
	return nil
}

func validAgentCustomName(name string) bool {
	if name == "" || len(name) > MaximumAgentCustomNameBytes {
		return false
	}
	for _, character := range name {
		valid := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '.' || character == '-'
		if !valid {
			return false
		}
	}
	return true
}

func (report AgentReport) Digest() (string, error) {
	report.CapturedAt = report.CapturedAt.UTC()
	sources, err := EncodeCustomSampleMetadata(report.CustomSamples)
	if err != nil {
		return "", err
	}
	if len(report.CustomSamples) > 0 {
		report.CustomSamples = nil
		if err := json.Unmarshal([]byte(sources), &report.CustomSamples); err != nil {
			return "", ErrAgentReportInvalid
		}
	}
	body, err := json.Marshal(report)
	if err != nil {
		return "", ErrAgentReportInvalid
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}
