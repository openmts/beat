package backup

import (
	"errors"
	"slices"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// validateArchivedMetricErasureJob 校验单行擦除任务；它只依赖模型常量，绝不读取样本。
func validateArchivedMetricErasureJob(
	job model.MetricErasureJob,
	nodes map[string]bool,
	tasks map[string]bool,
) error {
	if !model.ValidUUIDShape(job.ID) || !model.ValidUUIDShape(job.RequestID) ||
		!model.ValidMetricErasureScope(job.Scope) || !slices.Contains(store.MetricErasureStates(), job.State) {
		return errors.New("archived metric erasure job has an invalid identity or scope")
	}
	if job.TargetType != model.MetricErasureTargetTypeFor(job.Scope) || job.RequestedAt.IsZero() {
		return errors.New("archived metric erasure job has a mismatched target or time")
	}
	if job.AttemptCount < 0 || job.AttemptCount > store.MaximumMetricErasureAttempts {
		return errors.New("archived metric erasure job has an out-of-range attempt count")
	}
	if err := validateMetricErasureJobState(job); err != nil {
		return err
	}
	return validateMetricErasureJobLinkage(job, nodes, tasks)
}

func validateMetricErasureJobState(job model.MetricErasureJob) error {
	switch job.State {
	case model.MetricErasureSucceeded, model.MetricErasureFailed, model.MetricErasureCanceled:
		if job.CompletedAt == nil {
			return errors.New("archived metric erasure job is terminal without a completion time")
		}
	default:
		if job.CompletedAt != nil {
			return errors.New("archived metric erasure job is active with a completion time")
		}
	}
	if job.State == model.MetricErasureFailed && job.LastErrorCode == "" {
		return errors.New("archived metric erasure job failed without an error code")
	}
	switch job.State {
	case model.MetricErasureDeleting, model.MetricErasureCompacting,
		model.MetricErasureFinalizing, model.MetricErasureSucceeded:
		if job.CutoffUnixNano == nil || *job.CutoffUnixNano <= 0 {
			return errors.New("archived metric erasure job is missing its frozen cutoff")
		}
	}
	return nil
}

// validateMetricErasureJobLinkage 只要实体还没被收尾移除，就必须仍然存在于快照里。
func validateMetricErasureJobLinkage(
	job model.MetricErasureJob,
	nodes map[string]bool,
	tasks map[string]bool,
) error {
	switch job.Scope {
	case model.MetricErasureNodeHistory:
		if !nodes[job.TargetID] {
			return errors.New("archived metric erasure job references a missing node")
		}
	case model.MetricErasureDeleteNode:
		if job.State != model.MetricErasureSucceeded && !nodes[job.TargetID] {
			return errors.New("archived node deletion references a missing node")
		}
	case model.MetricErasureNetworkTaskHistory:
		if !tasks[job.TargetID] {
			return errors.New("archived metric erasure job references a missing quality task")
		}
	case model.MetricErasureDeleteNetworkTask:
		if job.State != model.MetricErasureSucceeded && !tasks[job.TargetID] {
			return errors.New("archived task deletion references a missing quality task")
		}
	case model.MetricErasureFleetTelemetry, model.MetricErasureAllMetricHistory:
		if job.TargetID != "" || job.TargetType != "" {
			return errors.New("archived fleet-wide erasure job carries a target")
		}
	}
	return nil
}
