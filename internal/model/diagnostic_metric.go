package model

import (
	"errors"
	"fmt"
	"strconv"
)

// 诊断测量名称：服务端只登记这些固定键，Agent 不得报告其他诊断测量。
const (
	DiagnosticMetricMTRLatencyMS        = "diagnostic_mtr_latency_ms"
	DiagnosticMetricMTRLossPercent      = "diagnostic_mtr_loss_percent"
	DiagnosticMetricMTRSentPackets      = "diagnostic_mtr_sent_packets"
	DiagnosticMetricMTRReceivedPackets  = "diagnostic_mtr_received_packets"
	DiagnosticMetricIperfBytesPerSecond = "diagnostic_iperf_bytes_per_second"
	DiagnosticMetricIperfBytes          = "diagnostic_iperf_bytes"
	DiagnosticMetricIperfRetransmits    = "diagnostic_iperf_retransmits"
	DiagnosticMetricIperfJitterMS       = "diagnostic_iperf_jitter_ms"
	DiagnosticMetricIperfLossPercent    = "diagnostic_iperf_loss_percent"
)

// 诊断测量的固定标签取值域。
const (
	DiagnosticStatLast  = "last"
	DiagnosticStatAvg   = "avg"
	DiagnosticStatBest  = "best"
	DiagnosticStatWorst = "worst"

	DiagnosticRoleSender   = "sender"
	DiagnosticRoleReceiver = "receiver"

	DiagnosticSampleField = "value"

	MaximumDiagnosticSampleBatch = 512
)

var (
	ErrDiagnosticSampleInvalid = errors.New("invalid diagnostic sample")
	ErrDiagnosticSampleLimit   = errors.New("diagnostic sample batch exceeds the limit")

	diagnosticMTROrdered = []string{DiagnosticMetricMTRLatencyMS, DiagnosticMetricMTRLossPercent,
		DiagnosticMetricMTRSentPackets, DiagnosticMetricMTRReceivedPackets}
	diagnosticIperfOrdered = []string{DiagnosticMetricIperfBytesPerSecond, DiagnosticMetricIperfBytes,
		DiagnosticMetricIperfRetransmits, DiagnosticMetricIperfJitterMS, DiagnosticMetricIperfLossPercent}
)

// DiagnosticMetrics 返回全部已登记的诊断测量名称。
func DiagnosticMetrics() []string {
	return append(append([]string{}, diagnosticMTROrdered...), diagnosticIperfOrdered...)
}

// ValidDiagnosticMetric 判断键是否为已登记的诊断测量。
func ValidDiagnosticMetric(key string) bool {
	return IsMTRDiagnosticMetric(key) || IsIperfDiagnosticMetric(key)
}

func IsMTRDiagnosticMetric(key string) bool { return containsKey(diagnosticMTROrdered, key) }

func IsIperfDiagnosticMetric(key string) bool { return containsKey(diagnosticIperfOrdered, key) }

func containsKey(keys []string, key string) bool {
	for _, candidate := range keys {
		if candidate == key {
			return true
		}
	}
	return false
}

// DiagnosticSample 是一条待写入 MTS 的数值诊断样本。
type DiagnosticSample struct {
	JobID     string  `json:"job_id"`
	NodeID    string  `json:"node_id"`
	Metric    string  `json:"metric"`
	Hop       int     `json:"hop,omitempty"`
	Stat      string  `json:"stat,omitempty"`
	Role      string  `json:"role,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Protocol  string  `json:"protocol,omitempty"`
	Value     float64 `json:"value"`
}

// Tags 返回该样本固定且有序的标签集合；无标签的键不会写入。
func (sample DiagnosticSample) Tags() (map[string]string, error) {
	if err := sample.validate(); err != nil {
		return nil, err
	}
	tags := map[string]string{"node": sample.NodeID, "job": sample.JobID}
	if IsMTRDiagnosticMetric(sample.Metric) {
		tags["hop"] = strconv.Itoa(sample.Hop)
		tags["stat"] = sample.Stat
		return tags, nil
	}
	tags["direction"] = sample.Direction
	tags["protocol"] = sample.Protocol
	tags["role"] = sample.Role
	return tags, nil
}

func (sample DiagnosticSample) validate() error {
	if sample.JobID == "" || len(sample.JobID) > MaximumDiagnosticReferenceBytes {
		return fmt.Errorf("%w: job identity is invalid", ErrDiagnosticSampleInvalid)
	}
	if sample.NodeID == "" || len(sample.NodeID) > MaximumDiagnosticReferenceBytes {
		return fmt.Errorf("%w: node identity is invalid", ErrDiagnosticSampleInvalid)
	}
	if !ValidDiagnosticMetric(sample.Metric) {
		return fmt.Errorf("%w: metric %q is not a diagnostic measurement", ErrDiagnosticSampleInvalid, sample.Metric)
	}
	if IsMTRDiagnosticMetric(sample.Metric) {
		if sample.Hop < 1 || sample.Hop > MaximumDiagnosticHops || !validDiagnosticStat(sample.Stat) {
			return fmt.Errorf("%w: hop or stat tag is invalid", ErrDiagnosticSampleInvalid)
		}
		return nil
	}
	if sample.Direction != DiagnosticDirectionForward && sample.Direction != DiagnosticDirectionReverse {
		return fmt.Errorf("%w: direction tag is invalid", ErrDiagnosticSampleInvalid)
	}
	if !validDiagnosticProtocol(sample.Protocol) || sample.Protocol == DiagnosticProtocolICMP {
		return fmt.Errorf("%w: protocol tag is invalid", ErrDiagnosticSampleInvalid)
	}
	if sample.Role != DiagnosticRoleSender && sample.Role != DiagnosticRoleReceiver {
		return fmt.Errorf("%w: role tag is invalid", ErrDiagnosticSampleInvalid)
	}
	return nil
}

func validDiagnosticStat(stat string) bool {
	switch stat {
	case DiagnosticStatLast, DiagnosticStatAvg, DiagnosticStatBest, DiagnosticStatWorst:
		return true
	default:
		return false
	}
}

// ValidateDiagnosticSamples 校验整批样本自身的合法性与有界数量。
func ValidateDiagnosticSamples(samples []DiagnosticSample) error {
	if len(samples) == 0 || len(samples) > MaximumDiagnosticSampleBatch {
		return ErrDiagnosticSampleLimit
	}
	for _, sample := range samples {
		if _, err := sample.Tags(); err != nil {
			return err
		}
	}
	return nil
}
