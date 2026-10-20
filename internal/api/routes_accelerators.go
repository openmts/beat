package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func WithAccelerators(service handler.AcceleratorService) RouterOption {
	return func(router *Router) { router.accelerators = service }
}

func (router *Router) registerAcceleratorRoutes(mux *http.ServeMux) {
	if router.accelerators == nil {
		return
	}
	h := handler.NewAcceleratorHandler(router.accelerators)
	mux.Handle("GET /api/v1/admin/nodes/{id}/accelerators", router.admin(http.HandlerFunc(h.HandleNode)))
	mux.Handle("GET /api/v1/admin/nodes/{id}/accelerator-policy", router.admin(http.HandlerFunc(h.HandlePolicy)))
	mux.Handle("PUT /api/v1/admin/nodes/{id}/accelerator-policy", router.metadataAdmin(http.HandlerFunc(h.HandleSavePolicy), true))
	mux.Handle("PUT /api/v1/admin/nodes/{id}/accelerator-visibility", router.metadataAdmin(http.HandlerFunc(h.HandleSaveVisibility), false))
	mux.Handle("GET /api/v1/admin/accelerator-enrollment-policy", router.ownerAdmin(http.HandlerFunc(h.HandlePolicy)))
	mux.Handle("PUT /api/v1/admin/accelerator-enrollment-policy", router.recentAuthOwnerAdmin(http.HandlerFunc(h.HandleSavePolicy)))
}
