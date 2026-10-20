package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func WithNotificationQueue(queue *notificationqueue.Service) RouterOption {
	return func(router *Router) { router.notificationQueue = queue }
}

func (router *Router) registerNotificationRoutes(mux *http.ServeMux) {
	if router.notificationQueue == nil {
		return
	}
	h := handler.NewNotificationHandler(router.notificationQueue)
	for path, handle := range map[string]http.HandlerFunc{
		"GET /api/v1/alerts/channels": h.HandleListChannels, "GET /api/v1/alerts/channels/{id}": h.HandleGetChannel,
		"PATCH /api/v1/alerts/channels/{id}/enabled": h.HandleSetEnabled,
		"GET /api/v1/notifications/deliveries":       h.HandleListDeliveries, "GET /api/v1/notifications/deliveries/{id}": h.HandleGetDelivery,
		"GET /api/v1/notifications/event-kinds": h.HandleEventKinds,
	} {
		mux.Handle(path, router.admin(handle))
	}
	mux.Handle("POST /api/v1/alerts/channels/{id}/test", router.serviceTokenPrivateResourceRoute(
		model.ServiceScopeNotificationTestSend, model.ServiceResourceChannel, middleware.ServicePathResource("id"), router.admin,
	)(http.HandlerFunc(h.HandleTestChannel)))
	if router.security == nil {
		return
	}
	mux.Handle("GET /api/v1/notifications/settings", router.ownerAdmin(http.HandlerFunc(h.HandleSettings)))
	for path, handle := range map[string]http.HandlerFunc{
		"POST /api/v1/alerts/channels": h.HandleCreateChannel, "PUT /api/v1/alerts/channels/{id}": h.HandleUpdateChannel,
		"DELETE /api/v1/alerts/channels/{id}": h.HandleDeleteChannel, "POST /api/v1/alerts/channels/preview": h.HandlePreview,
		"POST /api/v1/notifications/deliveries/{id}/retry": h.HandleRetry, "POST /api/v1/notifications/deliveries/{id}/cancel": h.HandleCancel,
		"PUT /api/v1/notifications/settings": h.HandleUpdateSettings,
	} {
		mux.Handle(path, router.recentAuthOwnerAdmin(handle))
	}
}
