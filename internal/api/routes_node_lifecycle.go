package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func WithGeoIP(service handler.GeoIPService, views handler.NodeLocationViews) RouterOption {
	return func(router *Router) { router.geoIP, router.locationViews = service, views }
}

func WithNodeLifecycle(service handler.NodeLifecycleService, views handler.NodeLifecycleViews) RouterOption {
	return func(router *Router) { router.nodeLifecycle, router.lifecycleViews = service, views }
}

func (router *Router) registerNodeLifecycleRoutes(mux *http.ServeMux) {
	if router.geoIP != nil {
		h := handler.NewGeoIPHandler(router.geoIP)
		mux.Handle("GET /api/v1/admin/settings/geoip", router.ownerAdmin(http.HandlerFunc(h.HandleSettings)))
		mux.Handle("GET /api/v1/admin/nodes/{id}/location", router.admin(http.HandlerFunc(h.HandleLocation)))
		for path, handle := range map[string]http.HandlerFunc{
			"PUT /api/v1/admin/settings/geoip":                h.HandleSaveSettings,
			"PUT /api/v1/admin/nodes/{id}/location":           h.HandleSaveLocation,
			"POST /api/v1/admin/nodes/{id}/location/refresh":  h.HandleRefresh,
			"POST /api/v1/admin/settings/geoip/mmdb":          h.HandleMMDBUpload,
			"POST /api/v1/admin/settings/geoip/mmdb/update":   h.HandleMMDBUpdate,
			"POST /api/v1/admin/settings/geoip/mmdb/rollback": h.HandleMMDBRollback,
		} {
			mux.Handle(path, router.recentAuthOwnerAdmin(handle))
		}
	}
	if router.nodeLifecycle != nil {
		router.registerLifecycleSettings(mux)
	}
}

func (router *Router) registerLifecycleSettings(mux *http.ServeMux) {
	h := handler.NewNodeLifecycleHandler(router.nodeLifecycle)
	mux.Handle("GET /api/v1/admin/settings/lifecycle", router.admin(http.HandlerFunc(h.HandleSettings)))
	mux.Handle("PUT /api/v1/admin/settings/lifecycle", router.recentAuthOwnerAdmin(http.HandlerFunc(h.HandleSaveSettings)))
	mux.Handle("GET /api/v1/admin/nodes/{id}/lifecycle", router.admin(http.HandlerFunc(h.HandleNode)))
	mux.Handle("PUT /api/v1/admin/nodes/{id}/lifecycle", router.admin(http.HandlerFunc(h.HandleSaveNode)))
	mux.Handle("GET /api/v1/admin/nodes/{id}/lifecycle/events", router.admin(http.HandlerFunc(h.HandleEvents)))
}
