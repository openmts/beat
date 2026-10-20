package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type DashboardPolicyReader func(context.Context, bool) (model.DashboardPolicy, error)

type MetricDashboardHandler struct {
	dashboards *store.DashboardStore
	nodes      *store.NodeStore
	policy     DashboardPolicyReader
}

func NewMetricDashboardHandler(dashboards *store.DashboardStore, nodes *store.NodeStore, policy DashboardPolicyReader) *MetricDashboardHandler {
	return &MetricDashboardHandler{dashboards: dashboards, nodes: nodes, policy: policy}
}

func (handler *MetricDashboardHandler) HandleDefault(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if handler.dashboards != nil && handler.policy == nil {
		respondDashboardError(w, errors.New("metric catalog is unavailable"))
		return
	}
	value, err := handler.defaultDashboard(request)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	if handler.policy != nil {
		policy, err := handler.policy(request.Context(), true)
		if err != nil {
			respondDashboardError(w, err)
			return
		}
		value.Definition = publicDashboardDefinition(value.Definition, policy)
	}
	value.OwnerUserID, value.UpdatedBy = "", ""
	JSONResponse(w, http.StatusOK, value)
}

func (handler *MetricDashboardHandler) defaultDashboard(request *http.Request) (*model.MetricDashboard, error) {
	nodeID := request.URL.Query().Get("node_id")
	if nodeID != "" {
		value, err := handler.nodeDashboard(request.Context(), nodeID)
		if err != nil || value != nil {
			return value, err
		}
	}
	if handler.dashboards != nil {
		value, err := handler.dashboards.Get(request.Context(), model.PublicDashboardID)
		if value != nil && value.Scope != "public" {
			return nil, store.ErrDashboardNotFound
		}
		if err != nil || value != nil {
			return value, err
		}
	}
	value := model.DefaultMetricDashboard("public")
	return &value, nil
}

func (handler *MetricDashboardHandler) nodeDashboard(ctx context.Context, nodeID string) (*model.MetricDashboard, error) {
	if handler.nodes == nil {
		return nil, store.ErrDashboardNotFound
	}
	node, err := handler.nodes.GetPublicNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, store.ErrDashboardNotFound
	}
	if handler.dashboards == nil {
		return nil, nil
	}
	value, err := handler.dashboards.Get(ctx, "node-"+nodeID)
	if value != nil && (value.Scope != "node" || value.NodeID != nodeID) {
		return nil, store.ErrDashboardNotFound
	}
	return value, err
}

func (handler *MetricDashboardHandler) HandleList(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.actor(w, request)
	if !ok {
		return
	}
	values, err := handler.dashboards.List(request.Context(), actor.UserID)
	if err != nil {
		respondDashboardError(w, err)
		return
	}
	for _, scope := range []string{"public", "admin"} {
		fallback := model.DefaultMetricDashboard(scope)
		if !containsDashboard(values, fallback.ID) {
			values = append(values, fallback)
		}
	}
	JSONResponse(w, http.StatusOK, values)
}

func containsDashboard(values []model.MetricDashboard, id string) bool {
	for _, value := range values {
		if value.ID == id {
			return true
		}
	}
	return false
}

func publicDashboardDefinition(definition model.DashboardDefinition, policy model.DashboardPolicy) model.DashboardDefinition {
	panels := []model.DashboardPanel{}
	for _, panel := range definition.Panels {
		series := []model.DashboardSeries{}
		for _, query := range panel.Series {
			if publicDashboardSeries(query, policy) {
				series = append(series, query)
			}
		}
		panel.Series = series
		candidate := definition
		candidate.Panels = []model.DashboardPanel{panel}
		if err := model.ValidateDashboardPolicy(candidate, policy); err == nil {
			panels = append(panels, panel)
		}
	}
	definition.Panels = panels
	return definition
}

func publicDashboardSeries(series model.DashboardSeries, policy model.DashboardPolicy) bool {
	metric, exists := policy.Metrics[series.Metric]
	if !exists || !metric.Public || !metric.Enabled {
		return false
	}
	return strings.HasPrefix(series.NodeID, "$") || policy.Nodes[series.NodeID]
}
