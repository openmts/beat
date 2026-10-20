package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const maxSnapshotExpirySeconds = 365 * 24 * 60 * 60

type snapshotRequest struct {
	DashboardID      string `json:"dashboard_id"`
	Name             string `json:"name"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

func (handler *DashboardCollaborationHandler) HandleListSnapshots(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.snapshots == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	values, err := handler.snapshots.List(request.Context(), actor.UserID)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, values)
}

func (handler *DashboardCollaborationHandler) HandleCreateSnapshot(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.snapshots == nil || handler.dashboards == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	var body snapshotRequest
	if err := decodeCollaborationBody(w, request, 4096, &body); err != nil {
		respondCollaborationError(w, err)
		return
	}
	dashboard, err := handler.snapshotSource(request, strings.TrimSpace(body.DashboardID), actor)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	expires, err := snapshotExpiry(body.ExpiresInSeconds)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	write := store.SnapshotWrite{Actor: actor, Value: model.DashboardSnapshot{
		Name: strings.TrimSpace(body.Name), DashboardID: dashboard.ID, Revision: max(dashboard.Revision, 1),
		Definition: dashboard.Definition, ExpiresAt: expires}}
	value, err := handler.snapshots.Create(request.Context(), write)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, value)
}

// snapshotSource 解析快照来源看板并校验分享权限。
func (handler *DashboardCollaborationHandler) snapshotSource(request *http.Request, id string, actor store.DashboardActor) (*model.MetricDashboard, error) {
	if id == "" || len(id) > 80 {
		return nil, errCollaborationBadRequest
	}
	value, err := handler.dashboards.Get(request.Context(), id)
	if err != nil {
		return nil, err
	}
	if value == nil {
		value = dashboardTarget(id)
	}
	if value == nil {
		return nil, store.ErrDashboardNotFound
	}
	if value.Scope == "personal" {
		if value.OwnerUserID != actor.UserID {
			return nil, store.ErrDashboardNotFound
		}
		return value, nil
	}
	if value.Scope == "public" || value.Scope == "node" {
		return value, nil
	}
	principal, exists := middleware.AdminPrincipal(request.Context())
	if !exists || principal.User.Role != model.AdminRoleOwner || !principal.Session.RecentlyAuthenticated(model.NowUTC()) {
		return nil, store.ErrDashboardForbidden
	}
	return value, nil
}

func snapshotExpiry(seconds int) (*time.Time, error) {
	if seconds < 0 || seconds > maxSnapshotExpirySeconds {
		return nil, errCollaborationBadRequest
	}
	if seconds == 0 {
		return nil, nil
	}
	at := model.NowUTC().Add(time.Duration(seconds) * time.Second)
	return &at, nil
}

func (handler *DashboardCollaborationHandler) HandleRevokeSnapshot(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	if handler.snapshots == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	if err := handler.snapshots.Revoke(request.Context(), request.PathValue("id"), actor); err != nil {
		respondCollaborationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *DashboardCollaborationHandler) HandlePublicSnapshot(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if handler.snapshots == nil {
		JSONError(w, http.StatusServiceUnavailable, "dashboard collaboration is unavailable")
		return
	}
	token := request.PathValue("token")
	if len(token) != 64 {
		JSONError(w, http.StatusNotFound, "dashboard snapshot not found")
		return
	}
	value, err := handler.snapshots.GetByToken(request.Context(), token)
	if err != nil {
		respondCollaborationError(w, err)
		return
	}
	value.CreatedBy = ""
	JSONResponse(w, http.StatusOK, value)
}
