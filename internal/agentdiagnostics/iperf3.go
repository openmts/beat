package agentdiagnostics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

const (
	maximumIperf3Bytes = 1 << 20
	maximumIperf3Lines = 4096
)

type iperf3Executor struct {
	path string
}

func (executor iperf3Executor) Kind() string { return model.DiagnosticKindIperf3 }

// Execute 以 JSON 流模式运行 iperf3，并按 interval 事件推送数值批次。
func (executor iperf3Executor) Execute(ctx context.Context, spec jobSpec, sink sink) (outcome, error) {
	arguments, err := iperf3Arguments(executor.path, spec)
	if err != nil {
		return terminalOutcome(model.DiagnosticStateFailed, "local_policy"), nil
	}
	output, runErr := runFixedCommand(ctx, commandSpec{path: executor.path, arguments: arguments,
		directory: spec.Directory, maximumBytes: maximumIperf3Bytes})
	if runErr != nil {
		return classifyRunError(ctx, runErr)
	}
	summary, err := streamIperf3(output, spec, sink)
	if err != nil {
		// 只有解析失败才是本地终态；上报失败要上抛，让调用方裁决为 unknown。
		if errors.Is(err, ErrExecution) {
			return terminalOutcome(model.DiagnosticStateFailed, "parse_failed"), nil
		}
		return outcome{}, err
	}
	if summary.empty() {
		return terminalOutcome(model.DiagnosticStatePartial, "missing_summary"), nil
	}
	if err := sink(batch{Samples: summary.finalSamples(spec)}); err != nil {
		return outcome{}, err
	}
	return outcome{State: model.DiagnosticStateSucceeded, ActualBytes: summary.actualBytes()}, nil
}

func iperf3Arguments(path string, spec jobSpec) ([]string, error) {
	if !validBinaryPath(path) || spec.Destination == "" || spec.Port < 1 || spec.Port > 65535 {
		return nil, ErrPolicy
	}
	arguments := []string{"-c", spec.Destination, "-p", strconv.Itoa(spec.Port), "-t", strconv.Itoa(spec.DurationSeconds),
		"-P", strconv.Itoa(spec.ParallelStreams), "-b", strconv.Itoa(spec.BitrateMbps) + "M", "-J", "--json-stream"}
	switch spec.Protocol {
	case model.DiagnosticProtocolTCP:
	case model.DiagnosticProtocolUDP:
		arguments = append(arguments, "-u")
	default:
		return nil, ErrPolicy
	}
	if spec.Direction == model.DiagnosticDirectionReverse {
		arguments = append(arguments, "-R")
	}
	return arguments, nil
}

type iperf3Sum struct {
	JitterMS    *float64 `json:"jitter_ms"`
	LostPercent *float64 `json:"lost_percent"`
	Retransmits *uint64  `json:"retransmits"`
	BitsPerSec  float64  `json:"bits_per_second"`
	Bytes       float64  `json:"bytes"`
	Seconds     float64  `json:"seconds"`
}

type iperf3Stream struct {
	BitsPerSec float64 `json:"bits_per_second"`
	Bytes      float64 `json:"bytes"`
	Sender     bool    `json:"sender"`
}

type iperf3Interval struct {
	JitterMS    *float64       `json:"jitter_ms"`
	LostPercent *float64       `json:"lost_percent"`
	Retransmits *uint64        `json:"retransmits"`
	Seconds     float64        `json:"seconds"`
	Bytes       float64        `json:"bytes"`
	BitsPerSec  float64        `json:"bits_per_second"`
	Streams     []iperf3Stream `json:"streams"`
	Sum         *iperf3Sum     `json:"sum"`
}

type iperf3End struct {
	Sum         *iperf3Sum `json:"sum"`
	SumSent     *iperf3Sum `json:"sum_sent"`
	SumReceived *iperf3Sum `json:"sum_received"`
}

type iperf3Message struct {
	Data    json.RawMessage `json:"data"`
	Event   string          `json:"event"`
	Error   string          `json:"error"`
	Version string          `json:"version"`
}

type iperf3Summary struct {
	sent, received *iperf3Sum
}

func (summary iperf3Summary) empty() bool { return summary.sent == nil && summary.received == nil }

func streamIperf3(output []byte, spec jobSpec, sink sink) (iperf3Summary, error) {
	summary := iperf3Summary{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > maximumIperf3Lines {
			return iperf3Summary{}, ErrExecution
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message iperf3Message
		if err := json.Unmarshal(line, &message); err != nil || message.Error != "" {
			return iperf3Summary{}, ErrExecution
		}
		switch message.Event {
		case "interval":
			interval, err := decodeInterval(message.Data)
			if err != nil {
				return iperf3Summary{}, err
			}
			if err := sink(batch{Samples: intervalSamples(interval, spec)}); err != nil {
				return iperf3Summary{}, err
			}
		case "end":
			end, err := decodeEnd(message.Data)
			if err != nil {
				return iperf3Summary{}, err
			}
			summary.sent, summary.received = end.SumSent, end.SumReceived
			if summary.empty() && end.Sum != nil {
				summary.sent = end.Sum
			}
		case "start":
		default:
		}
	}
	if err := scanner.Err(); err != nil {
		return iperf3Summary{}, ErrExecution
	}
	return summary, nil
}

func decodeInterval(raw json.RawMessage) (iperf3Interval, error) {
	var interval iperf3Interval
	if len(raw) == 0 || !json.Valid(raw) || json.Unmarshal(raw, &interval) != nil {
		return iperf3Interval{}, ErrExecution
	}
	if !finite(interval.BitsPerSec, interval.Bytes) {
		return iperf3Interval{}, ErrExecution
	}
	return interval, nil
}

func decodeEnd(raw json.RawMessage) (iperf3End, error) {
	var end iperf3End
	if len(raw) == 0 || !json.Valid(raw) || json.Unmarshal(raw, &end) != nil {
		return iperf3End{}, ErrExecution
	}
	return end, nil
}

// intervalSamples 按流方向区分发送与接收角色；缺失流信息时退回 sum。
func intervalSamples(interval iperf3Interval, spec jobSpec) []diagnosticprotocol.Sample {
	samples := make([]diagnosticprotocol.Sample, 0, 6)
	senderSeen, receiverSeen := false, false
	for _, stream := range interval.Streams {
		role := model.DiagnosticRoleReceiver
		if stream.Sender {
			role = model.DiagnosticRoleSender
			senderSeen = true
		} else {
			receiverSeen = true
		}
		samples = append(samples, throughputSamples(spec, role, stream.BitsPerSec, stream.Bytes, interval)...)
	}
	if !senderSeen && !receiverSeen {
		bits, bytesTotal := interval.BitsPerSec, interval.Bytes
		if interval.Sum != nil {
			bits, bytesTotal = interval.Sum.BitsPerSec, interval.Sum.Bytes
		}
		samples = append(samples, throughputSamples(spec, model.DiagnosticRoleSender, bits, bytesTotal, interval)...)
	}
	return samples
}

// throughputSamples 生成一个角色的吞吐与字节两个固定测量。
func throughputSamples(spec jobSpec, role string, bits, bytesTotal float64, interval iperf3Interval) []diagnosticprotocol.Sample {
	if math.IsNaN(bits) || math.IsInf(bits, 0) || bits < 0 || math.IsInf(bytesTotal, 0) || bytesTotal < 0 {
		return nil
	}
	samples := []diagnosticprotocol.Sample{
		{Metric: model.DiagnosticMetricIperfBytesPerSecond, Protocol: spec.Protocol, Direction: spec.Direction,
			Role: role, Value: bits / 8},
		{Metric: model.DiagnosticMetricIperfBytes, Protocol: spec.Protocol, Direction: spec.Direction,
			Role: role, Value: bytesTotal},
	}
	if interval.Retransmits != nil {
		samples = append(samples, diagnosticprotocol.Sample{Metric: model.DiagnosticMetricIperfRetransmits,
			Protocol: spec.Protocol, Direction: spec.Direction, Role: role, Value: float64(*interval.Retransmits)})
	}
	if interval.JitterMS != nil && *interval.JitterMS >= 0 {
		samples = append(samples, diagnosticprotocol.Sample{Metric: model.DiagnosticMetricIperfJitterMS,
			Protocol: spec.Protocol, Direction: spec.Direction, Role: role, Value: *interval.JitterMS})
	}
	if interval.LostPercent != nil && *interval.LostPercent >= 0 {
		samples = append(samples, diagnosticprotocol.Sample{Metric: model.DiagnosticMetricIperfLossPercent,
			Protocol: spec.Protocol, Direction: spec.Direction, Role: role, Value: *interval.LostPercent})
	}
	return samples
}

// finalSamples 输出最终摘要：发送方与接收方各自的吞吐与字节。
func (summary iperf3Summary) finalSamples(spec jobSpec) []diagnosticprotocol.Sample {
	samples := make([]diagnosticprotocol.Sample, 0, 12)
	if summary.sent != nil {
		samples = append(samples, sumSamples(spec, model.DiagnosticRoleSender, summary.sent)...)
	}
	if summary.received != nil {
		samples = append(samples, sumSamples(spec, model.DiagnosticRoleReceiver, summary.received)...)
	}
	return samples
}

func sumSamples(spec jobSpec, role string, sum *iperf3Sum) []diagnosticprotocol.Sample {
	interval := iperf3Interval{BitsPerSec: sum.BitsPerSec, Bytes: sum.Bytes, Retransmits: sum.Retransmits,
		JitterMS: sum.JitterMS, LostPercent: sum.LostPercent}
	return throughputSamples(spec, role, sum.BitsPerSec, sum.Bytes, interval)
}

func (summary iperf3Summary) actualBytes() int64 {
	total := 0.0
	for _, sum := range []*iperf3Sum{summary.sent, summary.received} {
		if sum == nil || sum.Bytes <= 0 || math.IsInf(sum.Bytes, 0) {
			continue
		}
		total += sum.Bytes
	}
	if total <= 0 {
		return 0
	}
	return int64(total)
}
