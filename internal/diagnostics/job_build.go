package diagnostics

import (
	"encoding/json"
	"strings"

	"github.com/beat/backend/internal/model"
)

// buildJob 生成不可变快照，并把请求参数收敛到策略、目标与能力的交集。
func buildJob(actor Actor, input JobInput, nodeName string, target model.DiagnosticTarget,
	policy model.DiagnosticPolicy, document CapabilityDocument) (model.DiagnosticJob, error) {
	if input.Protocol != target.Protocol || input.Direction != target.Direction {
		return model.DiagnosticJob{}, ErrForbidden
	}
	if target.IPFamily != model.IPFamilyAuto && input.IPFamily != model.IPFamilyAuto && input.IPFamily != target.IPFamily {
		return model.DiagnosticJob{}, ErrForbidden
	}
	if err := ensureCapability(document, input); err != nil {
		return model.DiagnosticJob{}, err
	}
	policySnapshot, err := encodeSnapshot(policy)
	if err != nil {
		return model.DiagnosticJob{}, err
	}
	job := model.DiagnosticJob{SourceNodeID: input.SourceNodeID, TargetID: target.ID, Kind: input.Kind,
		State: model.DiagnosticStateQueued, ActorUserID: actor.UserID, ActorUsername: actor.Username,
		Confirmation: input.Confirmation, SourceNodeName: nodeName, TargetName: target.Name,
		TargetRevision: target.Revision, PolicyRevision: policy.Revision, Protocol: input.Protocol,
		Direction: input.Direction, IPFamily: effectiveFamily(input.IPFamily, target.IPFamily),
		Probes: input.Probes, HopLimit: min(input.HopLimit, model.MaximumDiagnosticHops),
		DurationSeconds: min(effectiveDuration(policy, input.DurationSeconds), target.MaximumDuration),
		ParallelStreams: min(input.ParallelStreams, target.MaximumParallelStream),
		BitrateMbps:     min(effectiveBitrate(policy, input.BitrateMbps), target.MaximumBitrateMbps),
		PolicySnapshot:  policySnapshot}
	if job.Kind == model.DiagnosticKindMTR {
		job.DurationSeconds = 0
		job.ParallelStreams = 0
		job.BitrateMbps = 0
	}
	job.TargetSnapshot = encodeTargetSnapshot(target)
	job.CapabilityDigest = ""
	return job, nil
}

func ensureCapability(document CapabilityDocument, input JobInput) error {
	if !allowsKindIn(document.Kinds, input.Kind) {
		return ErrCapability
	}
	maxima := document.Maxima
	if input.Kind == model.DiagnosticKindMTR {
		if document.MTR == nil || !containsString(document.MTR.Protocols, input.Protocol) {
			return ErrCapability
		}
		if input.Probes > maxima.Probes || input.HopLimit > maxima.HopLimit {
			return ErrCapability
		}
		return nil
	}
	if document.Iperf3 == nil || !containsString(document.Iperf3.Protocols, input.Protocol) ||
		!containsString(document.Iperf3.Directions, input.Direction) {
		return ErrCapability
	}
	if input.DurationSeconds > maxima.DurationSeconds || input.ParallelStreams > maxima.ParallelStreams {
		return ErrCapability
	}
	return nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func effectiveFamily(requested, target string) string {
	if requested == "" || requested == model.IPFamilyAuto {
		return target
	}
	return requested
}

func encodeSnapshot(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalid
	}
	return string(encoded), nil
}

// encodeTargetSnapshot 快照保留审批元数据，便于审计回溯当时的边界。
func encodeTargetSnapshot(target model.DiagnosticTarget) string {
	snapshot := struct {
		ID          string `json:"id"`
		Revision    int    `json:"revision"`
		Address     string `json:"address"`
		Port        int    `json:"port"`
		Protocol    string `json:"protocol"`
		Direction   string `json:"direction"`
		IPFamily    string `json:"ip_family"`
		Private     bool   `json:"private_address"`
		Bitrate     int    `json:"maximum_bitrate_mbps"`
		Duration    int    `json:"maximum_duration_seconds"`
		Streams     int    `json:"maximum_parallel_streams"`
		KindName    string `json:"name"`
		DailyBudget int64  `json:"daily_byte_budget"`
	}{ID: target.ID, Revision: target.Revision, Address: target.Address, Port: target.Port,
		Protocol: target.Protocol, Direction: target.Direction, IPFamily: target.IPFamily,
		Private: target.PrivateAddress, Bitrate: target.MaximumBitrateMbps, Duration: target.MaximumDuration,
		Streams: target.MaximumParallelStream, KindName: strings.TrimSpace(target.Name), DailyBudget: target.DailyByteBudget}
	encoded, _ := json.Marshal(snapshot)
	return string(encoded)
}
