package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type dashboardRequest struct {
	Revision   int                       `json:"revision"`
	Definition model.DashboardDefinition `json:"definition"`
	// Summary 是可选的版本备注，随本次保存写进版本历史。
	Summary string `json:"summary"`
}

func (handler *MetricDashboardHandler) actor(w http.ResponseWriter, request *http.Request) (store.DashboardActor, bool) {
	principal, exists := middleware.AdminPrincipal(request.Context())
	if !exists {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return store.DashboardActor{}, false
	}
	if handler.dashboards == nil {
		JSONError(w, http.StatusServiceUnavailable, "metric dashboards are unavailable")
		return store.DashboardActor{}, false
	}
	return store.DashboardActor{UserID: principal.User.ID, SessionID: principal.Session.ID,
		RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request),
		UserAgent: request.UserAgent()}, true
}

func decodeDashboardRequest(w http.ResponseWriter, request *http.Request, value any) error {
	request.Body = http.MaxBytesReader(w, request.Body, model.MaxDashboardBytes+1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return model.ErrInvalidDashboard
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return model.ErrInvalidDashboard
	}
	return nil
}

func (handler *MetricDashboardHandler) writableDashboard(request *http.Request, actor store.DashboardActor) (*model.MetricDashboard, error) {
	id := request.PathValue("id")
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
	if value.Scope == "personal" && value.OwnerUserID != actor.UserID {
		return nil, store.ErrDashboardNotFound
	}
	if value.Scope != "personal" {
		if err := requireDashboardPublication(request); err != nil {
			return nil, err
		}
	}
	return value, nil
}

func dashboardTarget(id string) *model.MetricDashboard {
	scope := ""
	switch id {
	case model.PublicDashboardID:
		scope = "public"
	case model.AdminDashboardID:
		scope = "admin"
	}
	if scope != "" {
		value := model.DefaultMetricDashboard(scope)
		return &value
	}
	if strings.HasPrefix(id, "node-") && len(id) > len("node-") {
		value := model.DefaultMetricDashboard("node")
		value.ID, value.NodeID = id, strings.TrimPrefix(id, "node-")
		return &value
	}
	return nil
}

var errDashboardRecentAuth = errors.New("recent identity confirmation is required to publish a dashboard")

func requireDashboardPublication(request *http.Request) error {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok || principal.User.Role != model.AdminRoleOwner {
		return store.ErrDashboardForbidden
	}
	if !principal.Session.RecentlyAuthenticated(model.NowUTC()) {
		return errDashboardRecentAuth
	}
	return nil
}

func (handler *MetricDashboardHandler) validatePolicy(request *http.Request, value model.MetricDashboard) error {
	if handler.policy == nil {
		return errors.New("metric catalog is unavailable")
	}
	public := value.Scope == "public" || value.Scope == "node"
	policy, err := handler.policy(request.Context(), public)
	if err != nil {
		return err
	}
	if value.Scope == "node" && !policy.Nodes[value.NodeID] {
		return store.ErrDashboardNotFound
	}
	return model.ValidateDashboardPolicy(value.Definition, policy)
}

func respondDashboardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidDashboard):
		JSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrDashboardNotFound):
		JSONError(w, http.StatusNotFound, "metric dashboard not found")
	case errors.Is(err, store.ErrDashboardConflict):
		JSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrDashboardLimit):
		JSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrDashboardForbidden):
		JSONError(w, http.StatusForbidden, "metric dashboard permission denied")
	case errors.Is(err, errDashboardRecentAuth):
		JSONError(w, http.StatusPreconditionRequired, err.Error())
	default:
		JSONError(w, http.StatusServiceUnavailable, "metric dashboards are unavailable")
	}
}
