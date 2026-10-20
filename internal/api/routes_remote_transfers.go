package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
)

func registerRemoteTransferRoutes(mux *http.ServeMux, config OperationRouteConfig) {
	transfers, ok := config.Operations.(handler.RemoteTransfers)
	if !ok {
		return
	}
	h := handler.NewRemoteTransferHandler(transfers)
	for path, handle := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/operations/transfers":                h.HandleList,
		"POST /api/v1/admin/operations/transfers":               h.HandleCreate,
		"GET /api/v1/admin/operations/transfers/{id}":           h.HandleGet,
		"POST /api/v1/admin/operations/transfers/{id}/chunks":   h.HandleUpload,
		"POST /api/v1/admin/operations/transfers/{id}/finalize": h.HandleFinalize,
		"POST /api/v1/admin/operations/transfers/{id}/cancel":   h.HandleCancel,
		"DELETE /api/v1/admin/operations/transfers/{id}":        h.HandleDiscardDraft,
		"GET /api/v1/admin/operations/transfers/{id}/download":  h.HandleDownload,
	} {
		mux.Handle(path, config.ProtectAdmin(handle))
	}
	mux.HandleFunc("POST /api/v1/agent-operations/attempts/{id}/chunks", h.HandleAgentUpload)
	mux.HandleFunc("GET /api/v1/agent-operations/attempts/{id}/chunks", h.HandleAgentDownload)
}
