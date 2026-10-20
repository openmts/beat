package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// DashboardCollaborationHandler 承载 v25 看板协作面：注释、快照、面板库与面板级告警。
type DashboardCollaborationHandler struct {
	dashboards  *store.DashboardStore
	annotations *store.DashboardAnnotationStore
	snapshots   *store.DashboardSnapshotStore
	library     *store.DashboardPanelLibraryStore
	panelAlerts *store.DashboardPanelAlertStore
}

func NewDashboardCollaborationHandler(dashboards *store.DashboardStore, annotations *store.DashboardAnnotationStore,
	snapshots *store.DashboardSnapshotStore, library *store.DashboardPanelLibraryStore,
	panelAlerts *store.DashboardPanelAlertStore) *DashboardCollaborationHandler {
	return &DashboardCollaborationHandler{dashboards: dashboards, annotations: annotations,
		snapshots: snapshots, library: library, panelAlerts: panelAlerts}
}

func (handler *DashboardCollaborationHandler) actor(w http.ResponseWriter, request *http.Request) (store.DashboardActor, bool) {
	principal, exists := middleware.AdminPrincipal(request.Context())
	if !exists {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return store.DashboardActor{}, false
	}
	return store.DashboardActor{UserID: principal.User.ID, SessionID: principal.Session.ID,
		RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request),
		UserAgent: request.UserAgent()}, true
}

func decodeCollaborationBody(w http.ResponseWriter, request *http.Request, limit int64, value any) error {
	request.Body = http.MaxBytesReader(w, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errCollaborationBadRequest
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errCollaborationBadRequest
	}
	return nil
}

var errCollaborationBadRequest = errors.New("invalid dashboard collaboration request")

func respondCollaborationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCollaborationBadRequest), errors.Is(err, model.ErrInvalidDashboardAnnotation),
		errors.Is(err, model.ErrInvalidDashboardSnapshot), errors.Is(err, model.ErrInvalidLibraryPanel),
		errors.Is(err, model.ErrInvalidDashboard):
		JSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrAnnotationNotFound), errors.Is(err, store.ErrSnapshotNotFound),
		errors.Is(err, store.ErrLibraryPanelNotFound), errors.Is(err, store.ErrDashboardNotFound),
		errors.Is(err, store.ErrPanelAlertRuleNotFound):
		JSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrAnnotationLimit), errors.Is(err, store.ErrSnapshotLimit), errors.Is(err, store.ErrLibraryPanelLimit):
		JSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrDashboardForbidden):
		JSONError(w, http.StatusForbidden, "metric dashboard permission denied")
	default:
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
	}
}
