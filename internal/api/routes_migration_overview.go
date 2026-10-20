package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

// registerMigrationOverviewRoutes 只注册所有者会话可读的迁移与恢复视图。
func (router *Router) registerMigrationOverviewRoutes(api *http.ServeMux) {
	if router.migrationOverview == nil {
		return
	}
	overview := handler.NewMigrationOverviewHandler(router.migrationOverview)
	api.Handle("GET /api/v1/admin/migration",
		router.ownerAdmin(http.HandlerFunc(overview.HandleOverview)))
}
