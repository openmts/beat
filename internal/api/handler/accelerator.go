package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type AcceleratorService interface {
	Node(context.Context, store.AcceleratorActor, string) (model.NodeAcceleratorsAdmin, error)
	Policy(context.Context, store.AcceleratorActor, store.AcceleratorPolicyScope) (model.AcceleratorPolicy, error)
	SavePolicy(context.Context, store.AcceleratorActor, store.AcceleratorPolicyChange) (model.AcceleratorPolicy, error)
	AgentPolicy(context.Context, string, bool) (model.AcceleratorPolicy, error)
	PublishReport(context.Context, string, string) error
	PublicAccelerators(context.Context, string) ([]model.AcceleratorDevicePublic, error)
	PublicVisible(context.Context, string) (bool, error)
	SetVisibility(context.Context, store.AcceleratorActor, string, bool) error
}

type AcceleratorHandler struct{ service AcceleratorService }

func NewAcceleratorHandler(service AcceleratorService) *AcceleratorHandler {
	return &AcceleratorHandler{service: service}
}

func acceleratorActor(w http.ResponseWriter, r *http.Request) (store.AcceleratorActor, bool) {
	operationHeaders(w)
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "session_required")
		return store.AcceleratorActor{}, false
	}
	return store.AcceleratorActor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context()), IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()}, true
}

func acceleratorResponse(w http.ResponseWriter, value any, err error) {
	operationHeaders(w)
	if err == nil {
		JSONResponse(w, http.StatusOK, value)
		return
	}
	status, code := http.StatusServiceUnavailable, "accelerator_unavailable"
	switch {
	case errors.Is(err, store.ErrAcceleratorForbidden):
		status, code = http.StatusForbidden, "accelerator_forbidden"
	case errors.Is(err, store.ErrAdminMutationRecent):
		status, code = http.StatusPreconditionRequired, "recent_auth_required"
	case errors.Is(err, store.ErrAcceleratorPolicy):
		status, code = http.StatusConflict, "accelerator_policy_changed"
	case errors.Is(err, model.ErrAgentReportInvalid):
		status, code = http.StatusBadRequest, "accelerator_invalid"
	case errors.Is(err, sql.ErrNoRows):
		status, code = http.StatusNotFound, "not_found"
	}
	JSONError(w, status, code)
}

func (h *AcceleratorHandler) HandleNode(w http.ResponseWriter, r *http.Request) {
	actor, ok := acceleratorActor(w, r)
	if !ok {
		return
	}
	value, err := h.service.Node(r.Context(), actor, r.PathValue("id"))
	acceleratorResponse(w, value, err)
}

func (h *AcceleratorHandler) HandlePolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := acceleratorActor(w, r)
	if !ok {
		return
	}
	value, err := h.service.Policy(r.Context(), actor, acceleratorScope(r))
	acceleratorResponse(w, value, err)
}

func (h *AcceleratorHandler) HandleSavePolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := acceleratorActor(w, r)
	if !ok {
		return
	}
	var input model.AcceleratorPolicyWrite
	if err := decodeGeoIP(r, &input); err != nil {
		acceleratorResponse(w, nil, model.ErrAgentReportInvalid)
		return
	}
	value, err := h.service.SavePolicy(r.Context(), actor, store.AcceleratorPolicyChange{Scope: acceleratorScope(r), Write: input})
	acceleratorResponse(w, value, err)
}

func acceleratorScope(r *http.Request) store.AcceleratorPolicyScope {
	if id := r.PathValue("id"); id != "" {
		return store.AcceleratorPolicyScope{Kind: "node", ID: id}
	}
	return store.AcceleratorPolicyScope{Kind: "enrollment_policy", ID: "default"}
}

// HandleSaveVisibility 保存节点在指标页的 GPU 区域展示开关。
func (h *AcceleratorHandler) HandleSaveVisibility(w http.ResponseWriter, r *http.Request) {
	actor, ok := acceleratorActor(w, r)
	if !ok {
		return
	}
	var body struct {
		Visible bool `json:"visible"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	nodeID := r.PathValue("id")
	if err := h.service.SetVisibility(r.Context(), actor, nodeID, body.Visible); err != nil {
		acceleratorResponse(w, nil, err)
		return
	}
	JSONResponse(w, http.StatusOK, map[string]any{"node_id": nodeID, "visible": body.Visible})
}
