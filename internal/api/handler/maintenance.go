package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/maintenance"
	"github.com/beat/backend/internal/model"
)

type MaintenanceOperations interface {
	Overview(context.Context) (model.MaintenanceOverview, error)
	UpdateSettings(context.Context, model.MaintenanceSettings) (model.MaintenanceSettings, error)
	StartManual() error
}

type MaintenanceHandler struct {
	service MaintenanceOperations
}

func NewMaintenanceHandler(service MaintenanceOperations) *MaintenanceHandler {
	return &MaintenanceHandler{service: service}
}

func (handler *MaintenanceHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	overview, err := handler.service.Overview(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load maintenance settings")
		return
	}
	JSONResponse(w, http.StatusOK, overview)
}

func (handler *MaintenanceHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings model.MaintenanceSettings
	if err := ParseJSON(r, &settings); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := settings.Validate(); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := handler.service.UpdateSettings(r.Context(), settings)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update maintenance settings")
		return
	}
	JSONResponse(w, http.StatusOK, updated)
}

func (handler *MaintenanceHandler) HandleRun(w http.ResponseWriter, _ *http.Request) {
	if err := handler.service.StartManual(); err != nil {
		if errors.Is(err, maintenance.ErrAlreadyRunning) {
			JSONError(w, http.StatusConflict, err.Error())
			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to start maintenance")
		return
	}
	JSONResponse(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
