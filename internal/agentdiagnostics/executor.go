package agentdiagnostics

import (
	"context"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

const maximumSamplesPerBatch = 128

// jobSpec 是执行一次诊断所需的固定参数；没有任何自由文本命令。
type jobSpec struct {
	Destination     string
	Protocol        string
	Direction       string
	IPFamily        string
	Directory       string
	Port            int
	Probes          int
	HopLimit        int
	DurationSeconds int
	ParallelStreams int
	BitrateMbps     int
}

type batch struct {
	Samples []diagnosticprotocol.Sample
	Hops    []diagnosticprotocol.HopIdentity
}

// outcome 是本地执行的终态；unknown 只用于无法证明终止的场景。
type outcome struct {
	State       string
	FailureCode string
	ActualBytes int64
}

type sink func(batch) error

type executor interface {
	Kind() string
	Execute(context.Context, jobSpec, sink) (outcome, error)
}

func terminalOutcome(state, code string) outcome {
	return outcome{State: state, FailureCode: code}
}

// specFromLease 把服务端参数收敛到本地上限；超出即拒绝执行。
func specFromLease(config Config, lease *diagnosticprotocol.Lease, directory string) (jobSpec, error) {
	config = config.normalize()
	payload := lease.Payload
	spec := jobSpec{Destination: payload.Destination, Protocol: payload.Protocol, Direction: payload.Direction,
		IPFamily: payload.IPFamily, Directory: directory, Port: payload.Port, Probes: payload.Probes,
		HopLimit: payload.HopLimit, DurationSeconds: payload.DurationSeconds, ParallelStreams: payload.ParallelStreams,
		BitrateMbps: payload.BitrateMbps}
	if payload.Kind == model.DiagnosticKindMTR {
		if payload.Probes < model.MinimumDiagnosticProbes || payload.Probes > config.Maxima.Probes ||
			payload.HopLimit < 1 || payload.HopLimit > config.Maxima.HopLimit {
			return jobSpec{}, ErrPolicy
		}
		return spec, nil
	}
	if payload.Kind != model.DiagnosticKindIperf3 {
		return jobSpec{}, ErrPolicy
	}
	if payload.DurationSeconds < model.MinimumDiagnosticDurationSeconds || payload.DurationSeconds > config.Maxima.DurationSeconds ||
		payload.ParallelStreams < model.MinimumDiagnosticStreams || payload.ParallelStreams > config.Maxima.ParallelStreams ||
		payload.BitrateMbps < 1 || payload.BitrateMbps > config.Maxima.BitrateMbps {
		return jobSpec{}, ErrPolicy
	}
	return spec, nil
}
