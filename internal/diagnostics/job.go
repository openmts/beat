package diagnostics

import (
	"context"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// DiagnosticIperfConfirmation 是启动 iperf3 前必须显式提交的风险确认值。
const DiagnosticIperfConfirmation = "bandwidth-impact-accepted"

const jobColumns = `id, source_node_id, target_id, kind, state, actor_user_id, actor_username, confirmation,
	source_node_name, target_name, target_revision, policy_revision, protocol, direction, ip_family, probes,
	hop_limit, duration_seconds, parallel_streams, bitrate_mbps, policy_snapshot_json, target_snapshot_json,
	capability_digest, reserved_bytes, actual_bytes, attempt, cursor_sequence, cancel_requested, failure_code,
	created_at, updated_at, leased_at, started_at, completed_at`

// JobInput 是一次显式诊断请求；目的地只能是已审批目标。
type JobInput struct {
	SourceNodeID    string
	TargetID        string
	Kind            string
	Protocol        string
	Direction       string
	IPFamily        string
	Probes          int
	HopLimit        int
	DurationSeconds int
	ParallelStreams int
	BitrateMbps     int
	Confirmation    string
}

// CreateJob 在最近重认证后创建作业并事务性预留预算。
func (service *Service) CreateJob(ctx context.Context, actor Actor, input JobInput) (model.DiagnosticJob, error) {
	input = normalizeJobInput(input)
	if err := input.validate(); err != nil {
		return model.DiagnosticJob{}, ErrInvalid
	}
	nodeName := ""
	if service.labels != nil {
		name, err := service.labels.NodeName(ctx, input.SourceNodeID)
		if err != nil {
			return model.DiagnosticJob{}, ErrUnavailable
		}
		nodeName = name
	}
	var job model.DiagnosticJob
	err := service.write(ctx, func(tx transaction) error {
		if err := tx.authorize(actor, false, true); err != nil {
			return err
		}
		created, err := tx.createJob(actor, input, nodeName)
		if err != nil {
			return err
		}
		if err := tx.audit(actor.UserID, "diagnostic.job.created", created.ID); err != nil {
			return err
		}
		job = created
		return nil
	})
	return job, err
}

func (tx transaction) createJob(actor Actor, input JobInput, nodeName string) (model.DiagnosticJob, error) {
	policy, err := tx.policy()
	if err != nil {
		return model.DiagnosticJob{}, err
	}
	if !policy.Enabled || !allowsKind(policy, input.Kind) {
		return model.DiagnosticJob{}, ErrForbidden
	}
	if input.Kind == model.DiagnosticKindIperf3 && input.Confirmation != DiagnosticIperfConfirmation {
		return model.DiagnosticJob{}, ErrInvalid
	}
	target, err := tx.usableTarget(policy, input.Kind, input.TargetID)
	if err != nil {
		return model.DiagnosticJob{}, err
	}
	document, digest, err := tx.capability(input.SourceNodeID)
	if err != nil {
		return model.DiagnosticJob{}, err
	}
	job, err := buildJob(actor, input, nodeName, target, policy, document)
	if err != nil {
		return model.DiagnosticJob{}, err
	}
	job.CapabilityDigest = digest
	if err := tx.checkConcurrency(job, policy); err != nil {
		return model.DiagnosticJob{}, err
	}
	job.ReservedBytes = estimateJobBytes(job)
	if err := tx.reserveBudget(policy, target, job.SourceNodeID, job.ReservedBytes); err != nil {
		return model.DiagnosticJob{}, err
	}
	job.ID = uuid.NewString()
	job.CreatedAt, job.UpdatedAt = tx.now, tx.now
	if err := tx.insertJob(job); err != nil {
		return model.DiagnosticJob{}, err
	}
	return job, nil
}

func normalizeJobInput(input JobInput) JobInput {
	if input.Probes == 0 {
		input.Probes = model.DefaultDiagnosticProbes
	}
	if input.HopLimit == 0 {
		input.HopLimit = model.DefaultDiagnosticHops
	}
	if input.DurationSeconds == 0 {
		input.DurationSeconds = model.DefaultIperfDurationSeconds
	}
	if input.ParallelStreams == 0 {
		input.ParallelStreams = model.MinimumDiagnosticStreams
	}
	if input.BitrateMbps == 0 {
		input.BitrateMbps = model.DefaultDiagnosticBitrateMbps
	}
	if input.Protocol == "" {
		input.Protocol = model.DiagnosticProtocolICMP
	}
	if input.Direction == "" {
		input.Direction = model.DiagnosticDirectionForward
	}
	if input.IPFamily == "" {
		input.IPFamily = model.IPFamilyAuto
	}
	return input
}

func (input JobInput) validate() error {
	if input.Kind != model.DiagnosticKindMTR && input.Kind != model.DiagnosticKindIperf3 {
		return ErrInvalid
	}
	if input.SourceNodeID == "" || input.TargetID == "" ||
		len(input.SourceNodeID) > model.MaximumDiagnosticReferenceBytes || len(input.TargetID) > model.MaximumDiagnosticReferenceBytes {
		return ErrInvalid
	}
	if input.Kind == model.DiagnosticKindMTR {
		if input.Probes < model.MinimumDiagnosticProbes || input.Probes > model.MaximumDiagnosticProbes ||
			input.HopLimit < 1 || input.HopLimit > model.MaximumDiagnosticHops {
			return ErrInvalid
		}
		return nil
	}
	if input.DurationSeconds < model.MinimumDiagnosticDurationSeconds || input.DurationSeconds > model.HardMaximumDiagnosticDurationSeconds ||
		input.ParallelStreams < model.MinimumDiagnosticStreams || input.ParallelStreams > model.MaximumDiagnosticStreams {
		return ErrInvalid
	}
	if input.BitrateMbps < 1 || input.BitrateMbps > model.MaximumDiagnosticBitrateMbps {
		return ErrInvalid
	}
	return nil
}

func (tx transaction) insertJob(job model.DiagnosticJob) error {
	return tx.one(`INSERT INTO diagnostic_jobs(id, source_node_id, target_id, kind, state, actor_user_id,
		actor_username, confirmation, source_node_name, target_name, target_revision, policy_revision, protocol,
		direction, ip_family, probes, hop_limit, duration_seconds, parallel_streams, bitrate_mbps,
		policy_snapshot_json, target_snapshot_json, capability_digest, reserved_bytes, actual_bytes, attempt,
		cursor_sequence, cancel_requested, failure_code, created_at, updated_at, leased_at, started_at, completed_at)
	 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.SourceNodeID, job.TargetID, job.Kind, job.State, job.ActorUserID, job.ActorUsername,
		job.Confirmation, job.SourceNodeName, job.TargetName, job.TargetRevision, job.PolicyRevision, job.Protocol,
		job.Direction, job.IPFamily, job.Probes, job.HopLimit, job.DurationSeconds, job.ParallelStreams, job.BitrateMbps,
		job.PolicySnapshot, job.TargetSnapshot, job.CapabilityDigest, job.ReservedBytes, job.ActualBytes, job.Attempt,
		job.CursorSequence, false, job.ErrorCode, job.CreatedAt, job.UpdatedAt, nullableTime(job.LeasedAt),
		nullableTime(job.StartedAt), nullableTime(job.CompletedAt))
}

// checkConcurrency 在排队前强制全局、每节点与每目标的活动作业上限。
func (tx transaction) checkConcurrency(job model.DiagnosticJob, policy model.DiagnosticPolicy) error {
	rows, err := tx.tx.QueryContext(tx.ctx, `SELECT source_node_id, target_id FROM diagnostic_jobs
		WHERE state IN ('queued','leased','running','unknown')`)
	if err != nil {
		return ErrUnavailable
	}
	defer func() { _ = rows.Close() }()
	total := 0
	nodes := map[string]int{}
	targets := map[string]int{}
	for rows.Next() {
		var nodeID, targetID string
		if err := rows.Scan(&nodeID, &targetID); err != nil {
			return ErrUnavailable
		}
		total++
		nodes[nodeID]++
		targets[targetID]++
	}
	if err := rows.Err(); err != nil {
		return ErrUnavailable
	}
	if total >= policy.MaximumActiveJobs || nodes[job.SourceNodeID] >= policy.MaximumActiveJobsPerNode || targets[job.TargetID] >= 1 {
		return ErrConflict
	}
	return nil
}
