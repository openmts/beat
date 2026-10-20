package metricserasure

import (
	"context"
	"errors"
	"fmt"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// erase 执行删除、压缩与收尾三个阶段的编排。
func (service *Service) erase(ctx context.Context, claim store.MetricErasureClaim) error {
	job := claim.Job
	nodeID, taskID := erasureTarget(job)
	session, err := service.mts.BeginMetricErasure(ctx, job.Scope, nodeID, taskID)
	if err != nil {
		return service.fail(ctx, job, classifyEraseError(err))
	}
	defer session.Close()
	plan := session.Plan()
	if err := service.jobs.BeginDeleting(ctx, claim, plan.Digest, plan.CutoffUnixNano, service.now()); err != nil {
		return err
	}
	if err := service.deleteMeasurements(ctx, session, job, plan); err != nil {
		return service.fail(ctx, job, classifyEraseError(err))
	}
	if err := service.jobs.BeginCompacting(ctx, job.ID, service.now()); err != nil {
		return err
	}
	session.Close()
	if err := service.mts.CompactMetricErasure(ctx); err != nil {
		return service.fail(ctx, job, model.MetricErasureErrorCompaction)
	}
	if err := service.jobs.BeginFinalizing(ctx, job.ID, service.now()); err != nil {
		return err
	}
	if err := service.jobs.FinalizeEntity(ctx, job, service.now()); err != nil {
		return service.fail(ctx, job, model.MetricErasureErrorFinalize)
	}
	return service.jobs.Complete(ctx, job.ID, service.now())
}

// deleteMeasurements 从持久化的游标继续；每条测量删除成功后才推进游标。
func (service *Service) deleteMeasurements(
	ctx context.Context,
	session *store.MetricErasureSession,
	job model.MetricErasureJob,
	plan store.MetricErasurePlan,
) error {
	for _, measurement := range remainingMeasurements(plan.Measurements, job.MeasurementCursor) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := session.Erase(ctx, measurement); err != nil {
			return err
		}
		if err := service.jobs.Advance(ctx, job.ID, measurement, service.now()); err != nil {
			return err
		}
	}
	return nil
}

// remainingMeasurements 返回游标之后仍需删除的测量；游标不在清单里时从头开始。
func remainingMeasurements(measurements []string, cursor string) []string {
	if cursor == "" {
		return measurements
	}
	for index, measurement := range measurements {
		if measurement == cursor {
			return measurements[index+1:]
		}
	}
	return measurements
}

func erasureTarget(job model.MetricErasureJob) (string, string) {
	if job.TargetType == model.MetricErasureTargetNode {
		return job.TargetID, ""
	}
	if job.TargetType == model.MetricErasureTargetNetworkTask {
		return "", job.TargetID
	}
	return "", ""
}

func classifyEraseError(err error) string {
	switch {
	case errors.Is(err, store.ErrMetricErasureInventory):
		return model.MetricErasureErrorInventory
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return model.MetricErasureErrorLock
	case errors.Is(err, model.ErrMetricErasureInvalid):
		return model.MetricErasureErrorInternal
	default:
		return model.MetricErasureErrorMTS
	}
}

// fail 记录脱敏错误码；记录失败本身也要回传，避免调用方误判为已处理。
func (service *Service) fail(ctx context.Context, job model.MetricErasureJob, code string) error {
	if err := service.jobs.Fail(ctx, job.ID, code, service.now()); err != nil {
		return &codedError{code: code, err: fmt.Errorf("fail metric erasure job %s (%s): %w", job.ID, code, err)}
	}
	return &codedError{code: code, err: fmt.Errorf("metric erasure job %s failed: %s", job.ID, code)}
}
