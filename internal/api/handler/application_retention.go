package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/applicationgc"
	"github.com/beat/backend/internal/model"
)

// ApplicationRetentionOperations 暴露应用历史保留策略与手动清理。
type ApplicationRetentionOperations interface {
	Overview(context.Context) (model.ApplicationRetentionOverview, error)
	UpdateSettings(context.Context, model.ApplicationRetentionSettings) (model.ApplicationRetentionSettings, error)
	TriggerManual() error
}

type ApplicationRetentionHandler struct {
	service ApplicationRetentionOperations
}

func NewApplicationRetentionHandler(service ApplicationRetentionOperations) *ApplicationRetentionHandler {
	return &ApplicationRetentionHandler{service: service}
}

func (handler *ApplicationRetentionHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	overview, err := handler.service.Overview(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load application retention settings")
		return
	}
	JSONResponse(w, http.StatusOK, overview)
}

func (handler *ApplicationRetentionHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings model.ApplicationRetentionSettings
	if err := ParseJSONStrict(r, &settings); err != nil {
		WriteBodyError(w, err)
		return
	}
	if err := settings.Validate(); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := handler.service.UpdateSettings(r.Context(), settings)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update application retention settings")
		return
	}
	JSONResponse(w, http.StatusOK, updated)
}

func (handler *ApplicationRetentionHandler) HandleRun(w http.ResponseWriter, _ *http.Request) {
	if err := handler.service.TriggerManual(); err != nil {
		if errors.Is(err, applicationgc.ErrAlreadyRunning) {
			JSONError(w, http.StatusConflict, err.Error())
			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to start application history cleanup")
		return
	}
	JSONResponse(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
