package agentdiagnostics

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

const maximumMTRBytes = 1 << 20

type mtrExecutor struct {
	path string
}

func (executor mtrExecutor) Kind() string { return model.DiagnosticKindMTR }

// Execute 只调用固定参数：JSON 报告、受限跳数、显式地址族与地址字面量。
func (executor mtrExecutor) Execute(ctx context.Context, spec jobSpec, sink sink) (outcome, error) {
	arguments, err := mtrArguments(executor.path, spec)
	if err != nil {
		return terminalOutcome(model.DiagnosticStateFailed, "local_policy"), nil
	}
	output, runErr := runFixedCommand(ctx, commandSpec{path: executor.path, arguments: arguments,
		directory: spec.Directory, maximumBytes: maximumMTRBytes})
	if runErr != nil {
		return classifyRunError(ctx, runErr)
	}
	report, err := parseMTR(output)
	if err != nil {
		return terminalOutcome(model.DiagnosticStateFailed, "parse_failed"), nil
	}
	frames := mtrFrames(report, spec)
	for _, frame := range frames {
		if err := sink(frame); err != nil {
			return outcome{}, err
		}
	}
	state := model.DiagnosticStateSucceeded
	if !report.reached() {
		state = model.DiagnosticStatePartial
	}
	return terminalOutcome(state, ""), nil
}

func mtrArguments(path string, spec jobSpec) ([]string, error) {
	if !validBinaryPath(path) || spec.Destination == "" {
		return nil, ErrPolicy
	}
	arguments := []string{"-j", "-c", strconv.Itoa(spec.Probes), "-m", strconv.Itoa(spec.HopLimit)}
	arguments = append(arguments, "-4")
	if strings.Contains(spec.Destination, ":") {
		arguments[len(arguments)-1] = "-6"
	}
	switch spec.Protocol {
	case model.DiagnosticProtocolICMP:
	case model.DiagnosticProtocolTCP:
		arguments = append(arguments, "-T")
	case model.DiagnosticProtocolUDP:
		arguments = append(arguments, "-u")
	default:
		return nil, ErrPolicy
	}
	arguments = append(arguments, "--", spec.Destination)
	return arguments, nil
}

type mtrReport struct {
	Report struct {
		Hubs []mtrHub `json:"hubs"`
	} `json:"report"`
}

type mtrHub struct {
	LossPercentWrapped float64 `json:"Loss%"`
	Host               string  `json:"host"`
	LossPercent        float64 `json:"Loss"`
	Best               float64 `json:"Best"`
	Last               float64 `json:"Last"`
	Avg                float64 `json:"Avg"`
	Worst              float64 `json:"Wrst"`
	Sent               int     `json:"Snt"`
	Received           int     `json:"Recv"`
	Count              int     `json:"count"`
}

func (report mtrReport) reached() bool {
	for _, hub := range report.Report.Hubs {
		if hub.Received > 0 {
			return true
		}
	}
	return false
}

func parseMTR(output []byte) (mtrReport, error) {
	var report mtrReport
	if len(output) == 0 || len(output) > maximumMTRBytes || !json.Valid(output) {
		return mtrReport{}, ErrExecution
	}
	if err := json.Unmarshal(output, &report); err != nil {
		return mtrReport{}, ErrExecution
	}
	if len(report.Report.Hubs) == 0 || len(report.Report.Hubs) > model.MaximumDiagnosticHops {
		return mtrReport{}, ErrExecution
	}
	seen := map[int]bool{}
	for _, hub := range report.Report.Hubs {
		if hub.Count < 1 || hub.Count > model.MaximumDiagnosticHops || seen[hub.Count] {
			return mtrReport{}, ErrExecution
		}
		seen[hub.Count] = true
		if hub.Sent < 0 || hub.Received < 0 || hub.Received > hub.Sent || hub.Sent > model.MaximumDiagnosticProbes {
			return mtrReport{}, ErrExecution
		}
		if !finite(hub.Last, hub.Avg, hub.Best, hub.Worst) {
			return mtrReport{}, ErrExecution
		}
	}
	return report, nil
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return false
		}
	}
	return true
}

func mtrFrames(report mtrReport, spec jobSpec) []batch {
	hops := make([]diagnosticprotocol.HopIdentity, 0, len(report.Report.Hubs))
	samples := make([]diagnosticprotocol.Sample, 0, len(report.Report.Hubs)*6)
	for _, hub := range report.Report.Hubs {
		index := hub.Count
		hops = append(hops, diagnosticprotocol.HopIdentity{Index: index, Address: hub.Host, Reached: hub.Received > 0})
		samples = append(samples, mtrHopSamples(index, hub)...)
	}
	return []batch{{Hops: hops, Samples: samples}}
}

func mtrHopSamples(index int, hub mtrHub) []diagnosticprotocol.Sample {
	latency := func(stat string, value float64) diagnosticprotocol.Sample {
		return diagnosticprotocol.Sample{Metric: model.DiagnosticMetricMTRLatencyMS, Hop: index, Stat: stat, Value: value}
	}
	return []diagnosticprotocol.Sample{
		latency(model.DiagnosticStatLast, hub.Last), latency(model.DiagnosticStatAvg, hub.Avg),
		latency(model.DiagnosticStatBest, hub.Best), latency(model.DiagnosticStatWorst, hub.Worst),
		{Metric: model.DiagnosticMetricMTRLossPercent, Hop: index, Stat: model.DiagnosticStatLast, Value: mtrLoss(hub)},
		{Metric: model.DiagnosticMetricMTRSentPackets, Hop: index, Stat: model.DiagnosticStatLast, Value: float64(hub.Sent)},
		{Metric: model.DiagnosticMetricMTRReceivedPackets, Hop: index, Stat: model.DiagnosticStatLast, Value: float64(hub.Received)},
	}
}

func mtrLoss(hub mtrHub) float64 {
	if hub.LossPercentWrapped != 0 {
		return hub.LossPercentWrapped
	}
	return hub.LossPercent
}
