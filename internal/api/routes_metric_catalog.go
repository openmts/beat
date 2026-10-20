package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/store"
)

func WithMetricCatalogStore(catalog *store.MetricCatalogStore) RouterOption {
	return func(router *Router) {
		router.metricCatalog = catalog
		if catalog != nil && router.mtsStore != nil && router.nodeStore != nil {
			router.metricQueries = metricquery.NewService(catalog, router.nodeStore, router.mtsStore)
		}
	}
}

func (r *Router) registerMetricCatalogRoutes(api *http.ServeMux) {
	if r.metricCatalog == nil {
		return
	}
	h := handler.NewMetricCatalogHandler(r.metricCatalog, r.metricQueries, r.metricDispatcher)
	api.HandleFunc("GET /api/v1/metrics/catalog", h.HandlePublicCatalog)
	api.HandleFunc("POST /api/v1/metrics/query", h.HandlePublicQuery)
	api.Handle("GET /api/v1/admin/metrics/catalog", r.admin(http.HandlerFunc(h.HandleAdminCatalog)))
	api.Handle("POST /api/v1/admin/metrics/query", r.admin(http.HandlerFunc(h.HandleAdminQuery)))
	api.HandleFunc("POST /api/v1/metrics/query/latest", h.HandlePublicLatest)
	api.Handle("POST /api/v1/admin/metrics/query/latest", r.admin(http.HandlerFunc(h.HandleAdminLatest)))
	if r.security != nil {
		api.Handle("PUT /api/v1/admin/metrics/catalog/{key}", r.recentAuthOwnerAdmin(http.HandlerFunc(h.HandleUpdatePolicy)))
	} else {
		api.HandleFunc("PUT /api/v1/admin/metrics/catalog/{key}", func(w http.ResponseWriter, _ *http.Request) {
			handler.JSONError(w, http.StatusServiceUnavailable, "administrator authentication is unavailable")
		})
	}
}
