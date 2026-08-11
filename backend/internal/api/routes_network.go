package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func (r *Router) registerNetworkRoutes(api *http.ServeMux) {
	if r.networkTaskStore == nil || r.mtsStore == nil {
		return
	}
	networkHandler := handler.NewNetworkHandler(r.networkTaskStore, r.nodeStore, r.mtsStore)
	api.HandleFunc("GET /api/v1/network/quality", networkHandler.HandlePublicNetworkQuality)
	api.HandleFunc("GET /api/v1/network/quality/{task_id}/history", networkHandler.HandlePublicNetworkHistory)
	api.Handle("GET /api/v1/network/tasks", r.admin(http.HandlerFunc(networkHandler.HandleListNetworkTasks)))
	api.Handle("POST /api/v1/network/tasks", r.admin(http.HandlerFunc(networkHandler.HandleCreateNetworkTask)))
	api.Handle("PUT /api/v1/network/tasks/{task_id}", r.admin(http.HandlerFunc(networkHandler.HandleUpdateNetworkTask)))
	api.Handle("DELETE /api/v1/network/tasks/{task_id}", r.admin(http.HandlerFunc(networkHandler.HandleDeleteNetworkTask)))
	api.Handle("PUT /api/v1/network/tasks/sort", r.admin(http.HandlerFunc(networkHandler.HandleSortNetworkTasks)))
	api.Handle("GET /api/v1/network/tasks/{task_id}/history", r.admin(http.HandlerFunc(networkHandler.HandleAdminNetworkHistory)))
	api.Handle("GET /api/v1/network/assignments", r.agent(
		http.HandlerFunc(networkHandler.HandleNetworkAssignments), queryNodeName("node_name"),
	))
	api.Handle("POST /api/v1/network/results", r.agent(
		http.HandlerFunc(networkHandler.HandleNetworkResults), bodyNodeName("node_name"),
	))
}
