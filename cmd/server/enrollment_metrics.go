package main

import (
	"context"
	"time"

	"github.com/beat/backend/internal/observability"
	"github.com/beat/backend/internal/store"
)

// 入网清理结果枚举与 observability 的固定标签保持一致。
const (
	enrollmentCleanupCompleted = "completed"
	enrollmentCleanupFailed    = "failed"
)

// registerEnrollmentMetrics 把入网聚合接到 Prometheus 抓取；未初始化时保持空快照。
func registerEnrollmentMetrics(metrics *observability.Registry, stores *serverStores) {
	if metrics == nil || stores == nil || stores.sqlite == nil || stores.sqlite.DB == nil {
		return
	}
	enrollment := store.NewAgentEnrollmentStore(stores.sqlite.DB)
	metrics.SetEnrollmentStatistics(func() (observability.EnrollmentStatistics, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		statistics, err := enrollment.EnrollmentStatistics(ctx, time.Now().UTC())
		if err != nil {
			return observability.EnrollmentStatistics{}, err
		}
		return observability.EnrollmentStatistics{
			Pending: uint64(statistics.Pending), Approved: uint64(statistics.Approved),
			Rejected: uint64(statistics.Rejected), Expired: uint64(statistics.Expired),
			Claimed: uint64(statistics.Claimed), OldestPendingAge: statistics.OldestOpenRequestAge,
			ActiveGrants: uint64(statistics.ActiveGrants), ExpiredGrants: uint64(statistics.ExpiredGrants),
			DisabledGrants: uint64(statistics.DisabledGrants), RevokedGrants: uint64(statistics.RevokedGrants),
		}, nil
	})
}

// combineCleanupObserver 让清理指标观测与 worker 健康观测同时生效。
func combineCleanupObserver(worker func(error), registry *observability.Registry) func(error) {
	return func(err error) {
		worker(err)
		if err != nil {
			registry.ObserveEnrollmentCleanup(enrollmentCleanupFailed)
			return
		}
		registry.ObserveEnrollmentCleanup(enrollmentCleanupCompleted)
	}
}
