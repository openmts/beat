package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/nodelifecycle"
)

type NodeLifecycleService interface {
	Settings(context.Context, nodelifecycle.Actor) (model.LifecycleSettings, error)
	SaveSettings(context.Context, nodelifecycle.Actor, model.LifecycleSettings) (model.LifecycleSettings, error)
	Node(context.Context, nodelifecycle.Actor, string) (model.NodeLifecycle, error)
	SaveNode(context.Context, nodelifecycle.Actor, nodelifecycle.NodeChange) (model.NodeLifecycle, error)
	Events(context.Context, nodelifecycle.Actor, nodelifecycle.EventQuery) ([]model.LifecycleEvent, error)
}

type NodeLifecycleHandler struct{ service NodeLifecycleService }

func NewNodeLifecycleHandler(service NodeLifecycleService) *NodeLifecycleHandler {
	return &NodeLifecycleHandler{service: service}
}

func lifecycleActor(w http.ResponseWriter, r *http.Request) (nodelifecycle.Actor, bool) {
	operationHeaders(w)
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "session_required")
		return nodelifecycle.Actor{}, false
	}
	return nodelifecycle.Actor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context())}, true
}

func lifecycleResponse(w http.ResponseWriter, value any, err error) {
	operationHeaders(w)
	if err == nil {
		JSONResponse(w, http.StatusOK, value)
		return
	}
	status, code := http.StatusServiceUnavailable, "lifecycle_unavailable"
	switch {
	case errors.Is(err, nodelifecycle.ErrInvalid):
		status, code = http.StatusBadRequest, "lifecycle_invalid"
	case errors.Is(err, nodelifecycle.ErrForbidden):
		status, code = http.StatusForbidden, "lifecycle_forbidden"
	case errors.Is(err, nodelifecycle.ErrNotFound):
		status, code = http.StatusNotFound, "lifecycle_not_found"
	case errors.Is(err, nodelifecycle.ErrConflict):
		status, code = http.StatusConflict, "lifecycle_conflict"
	}
	JSONError(w, status, code)
}

func (h *NodeLifecycleHandler) HandleSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := lifecycleActor(w, r)
	if !ok {
		return
	}
	value, err := h.service.Settings(r.Context(), actor)
	lifecycleResponse(w, value, err)
}

func (h *NodeLifecycleHandler) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := lifecycleActor(w, r)
	if !ok {
		return
	}
	var input model.LifecycleSettings
	if err := decodeGeoIP(r, &input); err != nil {
		lifecycleResponse(w, nil, nodelifecycle.ErrInvalid)
		return
	}
	value, err := h.service.SaveSettings(r.Context(), actor, input)
	lifecycleResponse(w, value, err)
}

func (h *NodeLifecycleHandler) HandleNode(w http.ResponseWriter, r *http.Request) {
	actor, ok := lifecycleActor(w, r)
	if !ok {
		return
	}
	value, err := h.service.Node(r.Context(), actor, r.PathValue("id"))
	lifecycleResponse(w, value, err)
}

func (h *NodeLifecycleHandler) HandleSaveNode(w http.ResponseWriter, r *http.Request) {
	actor, ok := lifecycleActor(w, r)
	if !ok {
		return
	}
	var input model.LifecycleWrite
	if err := decodeGeoIP(r, &input); err != nil {
		lifecycleResponse(w, nil, nodelifecycle.ErrInvalid)
		return
	}
	value, err := h.service.SaveNode(r.Context(), actor, nodelifecycle.NodeChange{NodeID: r.PathValue("id"), LifecycleWrite: input})
	lifecycleResponse(w, value, err)
}

func (h *NodeLifecycleHandler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := lifecycleActor(w, r)
	if !ok {
		return
	}
	query := nodelifecycle.EventQuery{NodeID: r.PathValue("id"), Before: r.URL.Query().Get("before")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			lifecycleResponse(w, nil, nodelifecycle.ErrInvalid)
			return
		}
		query.Limit = value
	}
	value, err := h.service.Events(r.Context(), actor, query)
	lifecycleResponse(w, value, err)
}
