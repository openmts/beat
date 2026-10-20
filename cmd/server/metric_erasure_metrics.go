package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/beat/backend/internal/store"
)

// metricErasureStatusSource 让就绪检查与指标共用同一份计数快照。
type metricErasureStatusSource interface {
	Status(context.Context) (store.MetricErasureStatus, error)
}

// metricErasureCheck 报告擦除队列健康：排队与运行中的工作仍然算就绪，耗尽重试的
// 失败任务作为降级细节暴露给运维，而不是让整个实例不可用。
func (operations *operability) metricErasureCheck(ctx context.Context) readinessCheck {
	if operations.metricErasure == nil {
		return readinessCheck{Status: "ok", Detail: "disabled"}
	}
	if err := operations.metricErasure.Ready(ctx); err != nil {
		return readinessCheck{Status: "ok", Detail: "degraded: failed jobs require retry"}
	}
	return readinessCheck{Status: "ok"}
}

func (operations *operability) writeMetricErasureMetrics(output *strings.Builder, ctx context.Context) {
	if operations.metricErasureJobs == nil {
		return
	}
	status, err := operations.metricErasureJobs.Status(ctx)
	if err != nil {
		return
	}
	counts := status.Counts()
	_, _ = output.WriteString("# TYPE beat_metric_erasure_jobs gauge\n")
	for _, state := range store.MetricErasureStates() {
		_, _ = fmt.Fprintf(output, "beat_metric_erasure_jobs{state=%q} %d\n", state, counts[state])
	}
	_, _ = output.WriteString("# TYPE beat_metric_erasure_active_jobs gauge\n")
	for _, scope := range store.MetricErasureActiveScopes(status) {
		_, _ = fmt.Fprintf(output, "beat_metric_erasure_active_jobs{scope=%q} %d\n", scope.Name, scope.Count)
	}
}
