package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type MetricQueryOperations interface {
	Query(context.Context, metricquery.Request, metricquery.Access) (metricquery.Response, error)
	Latest(context.Context, metricquery.LatestRequest, metricquery.Access) (metricquery.LatestResponse, error)
}

type MetricCatalogHandler struct {
	catalog    *store.MetricCatalogStore
	queries    MetricQueryOperations
	dispatcher CustomMetricDispatcher
}

func NewMetricCatalogHandler(catalog *store.MetricCatalogStore, queries MetricQueryOperations, dispatcher CustomMetricDispatcher) *MetricCatalogHandler {
	return &MetricCatalogHandler{catalog: catalog, queries: queries, dispatcher: dispatcher}
}

func (h *MetricCatalogHandler) HandlePublicCatalog(w http.ResponseWriter, r *http.Request) {
	h.catalogResponse(w, r, true)
}

func (h *MetricCatalogHandler) HandleAdminCatalog(w http.ResponseWriter, r *http.Request) {
	h.catalogResponse(w, r, false)
}

func (h *MetricCatalogHandler) catalogResponse(w http.ResponseWriter, r *http.Request, publicOnly bool) {
	w.Header().Set("Cache-Control", "no-store")
	if h.catalog == nil {
		JSONError(w, http.StatusServiceUnavailable, "metric catalog is unavailable")
		return
	}
	catalog, err := h.catalog.Catalog(r.Context(), publicOnly)
	if err != nil {
		JSONError(w, http.StatusServiceUnavailable, "metric catalog is unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, catalog)
}

func (h *MetricCatalogHandler) HandleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.Role != model.AdminRoleOwner {
		JSONError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	if h.catalog == nil {
		JSONError(w, http.StatusServiceUnavailable, "metric catalog is unavailable")
		return
	}
	var policy model.MetricPolicyUpdate
	if !decodeMetricRequest(w, r, &policy) {
		return
	}
	change, err := h.catalog.UpdatePolicy(r.Context(), r.PathValue("key"), store.MetricCatalogPolicyChange{Policy: policy,
		SessionID: principal.Session.ID,
		Audit: model.AdminAuditEvent{ActorID: principal.User.ID, ActorUsername: principal.User.Username, RequestID: middleware.RequestID(r.Context()),
			IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()}})
	if err != nil {
		writeMetricPolicyError(w, err)
		return
	}
	if h.dispatcher != nil && len(change.Tasks) > 0 {
		if err := h.dispatcher.DispatchCustomMetricTasks(context.WithoutCancel(r.Context()), change.Tasks); err != nil {
			slog.WarnContext(r.Context(), "catalog stop tasks remain pending", "error", err)
		}
	}
	JSONResponse(w, http.StatusOK, change.Definition)
}

func writeMetricPolicyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrMetricCatalogForbidden):
		JSONError(w, http.StatusForbidden, "current owner session and recent authentication are required")
	case errors.Is(err, store.ErrMetricCatalogConflict):
		JSONError(w, http.StatusConflict, "metric policy revision has changed")
	case errors.Is(err, store.ErrMetricCatalogNotFound):
		JSONError(w, http.StatusNotFound, "metric definition not found")
	case errors.Is(err, metriccatalog.ErrInvalidDefinition):
		JSONError(w, http.StatusBadRequest, err.Error())
	default:
		JSONError(w, http.StatusInternalServerError, "metric policy update failed")
	}
}
