package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/store"
)

func WithMetricDashboards(dashboards *store.DashboardStore, policy handler.DashboardPolicyReader) RouterOption {
	return func(router *Router) { router.dashboards, router.dashboardPolicy = dashboards, policy }
}

func (router *Router) registerMetricDashboardRoutes(mux *http.ServeMux) {
	dashboards := handler.NewMetricDashboardHandler(router.dashboards, router.nodeStore, router.dashboardPolicy)
	mux.HandleFunc("GET /api/v1/metrics/dashboards/default", dashboards.HandleDefault)
	mux.Handle("GET /api/v1/admin/metrics/dashboards", router.admin(http.HandlerFunc(dashboards.HandleList)))
	mux.Handle("POST /api/v1/admin/metrics/dashboards", router.admin(http.HandlerFunc(dashboards.HandleCreate)))
	mux.Handle("PUT /api/v1/admin/metrics/dashboards/{id}", router.admin(http.HandlerFunc(dashboards.HandleUpdate)))
	mux.Handle("POST /api/v1/admin/metrics/dashboards/{id}/rollback", router.admin(http.HandlerFunc(dashboards.HandleRollback)))
	mux.Handle("GET /api/v1/admin/metrics/dashboards/{id}/versions", router.admin(http.HandlerFunc(dashboards.HandleListVersions)))
	mux.Handle("GET /api/v1/admin/metrics/dashboards/{id}/versions/{revision}", router.admin(http.HandlerFunc(dashboards.HandleVersionDefinition)))
	mux.Handle("POST /api/v1/admin/metrics/dashboards/{id}/versions/{revision}/restore", router.admin(http.HandlerFunc(dashboards.HandleRestoreVersion)))
	mux.Handle("DELETE /api/v1/admin/metrics/dashboards/{id}", router.admin(http.HandlerFunc(dashboards.HandleDelete)))
	router.registerDashboardCollaborationRoutes(mux)
}

// registerDashboardCollaborationRoutes 挂载 v25 看板协作面：注释、快照、面板库与面板级告警。
func (router *Router) dashboardCollaborationHandler() *handler.DashboardCollaborationHandler {
	if router.dashboards == nil {
		return handler.NewDashboardCollaborationHandler(nil, nil, nil, nil, nil)
	}
	database := router.dashboards.DB()
	return handler.NewDashboardCollaborationHandler(router.dashboards,
		store.NewDashboardAnnotationStore(database), store.NewDashboardSnapshotStore(database),
		store.NewDashboardPanelLibraryStore(database), store.NewDashboardPanelAlertStore(database))
}

func (router *Router) registerDashboardCollaborationRoutes(mux *http.ServeMux) {
	collaboration := router.dashboardCollaborationHandler()
	mux.HandleFunc("GET /api/v1/metrics/annotations", collaboration.HandlePublicAnnotations)
	mux.HandleFunc("GET /api/v1/metrics/snapshots/{token}", collaboration.HandlePublicSnapshot)
	mux.Handle("GET /api/v1/admin/metrics/annotations", router.admin(http.HandlerFunc(collaboration.HandleListAnnotations)))
	mux.Handle("POST /api/v1/admin/metrics/annotations", router.admin(http.HandlerFunc(collaboration.HandleCreateAnnotation)))
	mux.Handle("PUT /api/v1/admin/metrics/annotations/{id}", router.admin(http.HandlerFunc(collaboration.HandleUpdateAnnotation)))
	mux.Handle("DELETE /api/v1/admin/metrics/annotations/{id}", router.admin(http.HandlerFunc(collaboration.HandleDeleteAnnotation)))
	mux.Handle("GET /api/v1/admin/metrics/snapshots", router.admin(http.HandlerFunc(collaboration.HandleListSnapshots)))
	mux.Handle("POST /api/v1/admin/metrics/snapshots", router.admin(http.HandlerFunc(collaboration.HandleCreateSnapshot)))
	mux.Handle("DELETE /api/v1/admin/metrics/snapshots/{id}", router.admin(http.HandlerFunc(collaboration.HandleRevokeSnapshot)))
	mux.Handle("GET /api/v1/admin/metrics/panels", router.admin(http.HandlerFunc(collaboration.HandleListLibraryPanels)))
	mux.Handle("POST /api/v1/admin/metrics/panels", router.admin(http.HandlerFunc(collaboration.HandleCreateLibraryPanel)))
	mux.Handle("PUT /api/v1/admin/metrics/panels/{id}", router.admin(http.HandlerFunc(collaboration.HandleUpdateLibraryPanel)))
	mux.Handle("DELETE /api/v1/admin/metrics/panels/{id}", router.admin(http.HandlerFunc(collaboration.HandleDeleteLibraryPanel)))
	mux.Handle("GET /api/v1/admin/metrics/dashboards/{id}/panel-alerts", router.admin(http.HandlerFunc(collaboration.HandleListPanelAlerts)))
	mux.Handle("PUT /api/v1/admin/metrics/dashboards/{id}/panel-alerts/{panelId}", router.admin(http.HandlerFunc(collaboration.HandleReplacePanelAlerts)))
}
