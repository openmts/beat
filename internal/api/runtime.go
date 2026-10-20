package api

import (
	"context"
	"net/http"

	"github.com/beat/backend/internal/metricsfanout"
)

// 接线必须在开始ServeHTTP前完成，连接无法调用builder。
func (r *Router) SetMetricsStream(stream http.Handler) { r.metricsHandler.WithMetricsStream(stream) }

func (r *Router) BuildMetricsSnapshot(ctx context.Context) (metricsfanout.Snapshot, error) {
	return r.metricsHandler.BuildMetricsSnapshot(ctx)
}

func WithRuntimeStatus(status http.Handler) RouterOption {
	return func(r *Router) { r.runtimeStatus = status }
}
