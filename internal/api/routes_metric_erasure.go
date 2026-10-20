package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

// registerMetricErasureRoutes 只注册 owner 专属的擦除任务读写；全部要求最近认证。
func (router *Router) registerMetricErasureRoutes(mux *http.ServeMux) {
	if router.metricErasure == nil {
		return
	}
	h := handler.NewMetricErasureHandler(router.metricErasure)
	if router.metricErasureWake != nil {
		h = h.WithSignal(router.metricErasureWake)
	}
	owner := func(fn http.HandlerFunc) http.Handler { return router.metadataOwner(fn) }
	mux.Handle("GET /api/v1/admin/metric-erasure/jobs", owner(h.HandleList))
	mux.Handle("GET /api/v1/admin/metric-erasure/jobs/{job_id}", owner(h.HandleGet))
	mux.Handle("POST /api/v1/admin/metric-erasure/jobs", owner(h.HandleCreate))
	mux.Handle("POST /api/v1/admin/metric-erasure/jobs/{job_id}/retry", owner(h.HandleRetry))
	mux.Handle("POST /api/v1/admin/metric-erasure/jobs/{job_id}/cancel", owner(h.HandleCancel))
}
