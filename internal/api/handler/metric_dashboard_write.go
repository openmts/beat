package handler

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (handler *MetricDashboardHandler) HandleCreate(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	var body dashboardRequest
	if err := decodeDashboardRequest(w, request, &body); err != nil {
		respondDashboardError(w, err)
		return
	}
	value := model.MetricDashboard{ID: uuid.NewString(), Scope: "personal", OwnerUserID: actor.UserID, Definition: body.Definition}
	handler.saveDashboard(w, request, store.DashboardWrite{Value: value, ExpectedRevision: body.Revision, Actor: actor, Summary: body.Summary})
}

func (handler *MetricDashboardHandler) HandleUpdate(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	value, err := handler.writableDashboard(request, actor)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	var body dashboardRequest
	if err := decodeDashboardRequest(w, request, &body); err != nil {
		respondDashboardError(w, err)
		return
	}
	value.Definition = body.Definition
	handler.saveDashboard(w, request, store.DashboardWrite{Value: *value, ExpectedRevision: body.Revision, Actor: actor, Summary: body.Summary})
}

func (handler *MetricDashboardHandler) saveDashboard(w http.ResponseWriter, request *http.Request, write store.DashboardWrite) {
	if err := handler.validatePolicy(request, write.Value); err != nil {
		respondDashboardError(w, err)
		return
	}
	value, err := handler.dashboards.Save(request.Context(), write)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	status := http.StatusOK
	if request.Method == http.MethodPost {
		status = http.StatusCreated
	}
	JSONResponse(w, status, value)
}

func (handler *MetricDashboardHandler) HandleRollback(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	value, err := handler.writableDashboard(request, actor)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	var body struct {
		Revision int `json:"revision"`
	}
	if err := decodeDashboardRequest(w, request, &body); err != nil {
		respondDashboardError(w, err)
		return
	}
	previous, err := handler.dashboards.Previous(request.Context(), value.ID)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	value.Definition = *previous
	if err := handler.validatePolicy(request, *value); err != nil {
		respondDashboardError(w, err)
		return
	}
	value, err = handler.dashboards.Rollback(request.Context(), store.DashboardDelete{ID: value.ID, ExpectedRevision: body.Revision, Actor: actor})
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, value)
}

func (handler *MetricDashboardHandler) HandleDelete(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	revision, err := strconv.Atoi(request.URL.Query().Get("revision"))
	if err != nil || revision < 1 {
		respondDashboardError(w, model.ErrInvalidDashboard)
		return
	}
	value, err := handler.writableDashboard(request, actor)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	if err := handler.dashboards.Delete(request.Context(), store.DashboardDelete{ID: value.ID, ExpectedRevision: revision, Actor: actor}); err != nil {
		respondDashboardError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
