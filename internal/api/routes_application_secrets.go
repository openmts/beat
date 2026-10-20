package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func WithApplicationSecrets(secrets handler.ApplicationSecretOperations) RouterOption {
	return func(router *Router) { router.applicationSecrets = secrets }
}

func (router *Router) registerApplicationSecretRoutes(mux *http.ServeMux) {
	if router.applicationSecrets == nil || router.security == nil {
		return
	}
	handler := handler.NewApplicationSecretHandler(router.applicationSecrets)
	mux.Handle("GET /api/v1/admin/application-secrets", router.ownerAdmin(http.HandlerFunc(handler.HandleStatus)))
	mux.Handle("POST /api/v1/admin/application-secrets/rotate", router.recentAuthOwnerAdmin(http.HandlerFunc(handler.HandleRotate)))
}
