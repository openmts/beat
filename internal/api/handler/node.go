package handler

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

type NodeHandler struct {
	nodeStore     *store.NodeStore
	mtsStore      *store.MTSStore
	settingsStore *store.SiteSettingsStore
	taskEnqueuer  AgentTaskEnqueuer
	agentHub      *service.AgentHub
}

// AgentTaskEnqueuer queues agent tasks for a node.
type AgentTaskEnqueuer interface {
	EnqueueAuthorizedKeyTask(context.Context, string, string) (*model.AgentTask, error)
}

func NewNodeHandler(
	nodeStore *store.NodeStore,
	mtsStore *store.MTSStore,
	settingsStores ...*store.SiteSettingsStore,
) *NodeHandler {
	handler := &NodeHandler{nodeStore: nodeStore, mtsStore: mtsStore}
	if len(settingsStores) > 0 {
		handler.settingsStore = settingsStores[0]
	}
	return handler
}

func (h *NodeHandler) WithTaskEnqueuer(enqueuer AgentTaskEnqueuer) *NodeHandler {
	h.taskEnqueuer = enqueuer
	return h
}

func (h *NodeHandler) WithAgentHub(hub *service.AgentHub) *NodeHandler {
	h.agentHub = hub
	return h
}

func (h *NodeHandler) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	groupID := r.URL.Query().Get("group_id")

	nodes, err := h.nodeStore.ListPublicNodes(r.Context(), groupID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list nodes")

		return
	}
	settings, err := loadSiteSettings(r.Context(), h.settingsStore)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load site settings")
		return
	}

	output := make([]nodeResponse, 0, len(nodes))
	for _, node := range nodes {
		response, err := buildNodeResponse(r.Context(), publicNode(node, settings), h.mtsStore)
		if err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to load node metrics")
			return
		}
		output = append(output, response)
	}

	JSONResponse(w, http.StatusOK, output)
}

type metricResponse struct {
	NodeID    string  `json:"node_id"`
	Metric    string  `json:"metric"`
	Value     float64 `json:"value"`
	Timestamp int64   `json:"timestamp"`
}

var defaultMetrics = model.MetricNames()

func (h *NodeHandler) HandleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	node, err := h.nodeStore.GetPublicNode(r.Context(), id)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get node")

		return
	}

	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")

		return
	}
	settings, err := loadSiteSettings(r.Context(), h.settingsStore)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load site settings")
		return
	}

	response, err := buildNodeResponse(r.Context(), publicNode(*node, settings), h.mtsStore)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load node metrics")
		return
	}
	JSONResponse(w, http.StatusOK, response)
}

func (h *NodeHandler) HandleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body nodeUpdateRequest
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}
	if err := validateTrafficUpdate(body.TrafficLimit, body.TrafficLimitType, body.TrafficResetDay); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid traffic policy")

		return
	}
	if err := body.normalizePresentation(); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid node presentation")
		return
	}

	previous, err := h.nodeStore.GetNode(r.Context(), id)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update node")
		return
	}

	node, err := h.nodeStore.UpdateNode(r.Context(), id, body.storeUpdate())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update node")

		return
	}

	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")

		return
	}

	h.maybeDistributeKey(r.Context(), node, previous)

	JSONResponse(w, http.StatusOK, node)
}

func (h *NodeHandler) maybeDistributeKey(ctx context.Context, node *model.Node, previous *model.Node) {
	if h.taskEnqueuer == nil || node.SSHPublicKey == "" {
		return
	}
	if previous != nil && previous.SSHPublicKey == node.SSHPublicKey {
		return
	}
	task, err := h.taskEnqueuer.EnqueueAuthorizedKeyTask(ctx, node.ID, node.SSHPublicKey)
	if err != nil {
		slog.WarnContext(ctx, "enqueue authorized key task failed", "node_id", node.ID, "error", err)
		return
	}
	slog.InfoContext(ctx, "enqueued authorized key task",
		"node_id", node.ID, "task_id", task.ID, "online", h.agentHub != nil && h.agentHub.Online(node.ID))
	if h.agentHub != nil && !h.agentHub.PushTask(ctx, *task) {
		slog.DebugContext(ctx, "agent is offline, task will be replayed on reconnect",
			"node_id", node.ID, "task_id", task.ID)
	}
}

func validateTrafficUpdate(limit *int64, limitType *string, resetDay *int) error {
	policy := model.TrafficPolicy{
		Limit:     0,
		LimitType: model.TrafficLimitSum,
		ResetDay:  1,
	}
	if limit != nil {
		policy.Limit = *limit
	}
	if limitType != nil {
		policy.LimitType = *limitType
	}
	if resetDay != nil {
		policy.ResetDay = *resetDay
	}
	return policy.Validate()
}

func (h *NodeHandler) HandleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.nodeStore.DeleteNode(r.Context(), id); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to delete node")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *NodeHandler) HandleNodeReport(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Name          string             `json:"name"`
		Host          string             `json:"host"`
		Port          int                `json:"port"`
		System        model.SystemInfo   `json:"system"`
		Metrics       *model.NodeMetrics `json:"metrics"`
		CustomMetrics map[string]float64 `json:"custom_metrics"`
	}
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if body.Host == "" {
		JSONError(w, http.StatusBadRequest, "host is required")

		return
	}
	if body.Port < 1 || body.Port > 65535 {
		JSONError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return
	}
	if body.Metrics != nil {
		if err := body.Metrics.Validate(); err != nil {
			JSONError(w, http.StatusBadRequest, "invalid metrics")
			return
		}
	}

	node, err := h.nodeStore.UpdateNodeHeartbeat(r.Context(), identity.NodeID, store.NodeHeartbeat{
		Name: identity.NodeName, Host: body.Host, Port: body.Port, System: body.System,
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update node")

		return
	}
	if node == nil {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if body.Metrics != nil {
		now := model.NowUTC()

		if h.mtsStore == nil {
			JSONError(w, http.StatusServiceUnavailable, "metrics storage is unavailable")
			return
		}
		if err := h.mtsStore.WriteNodeMetrics(r.Context(), store.NodeMetricSample{
			NodeID: node.ID, Metrics: *body.Metrics, Timestamp: now,
		}); err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to store node metrics")
			return
		}
	}

	for name, value := range body.CustomMetrics {
		if !isValidCustomMetricName(name) || !isFiniteMetricValue(value) {
			JSONError(w, http.StatusBadRequest, "invalid custom metrics")
			return
		}
		if err := h.mtsStore.WriteMetric(r.Context(), node.ID, model.CustomMetricPrefix+name, value, model.NowUTC()); err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to store custom metric")
			return
		}
	}

	JSONResponse(w, http.StatusCreated, node)
}

func isValidCustomMetricName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func isFiniteMetricValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (h *NodeHandler) HandleGetNodeMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	node, err := h.nodeStore.GetPublicNode(r.Context(), nodeID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get node")
		return
	}
	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")
		return
	}

	metricParam := r.URL.Query().Get("metric")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid from timestamp")

		return
	}

	to, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid to timestamp")

		return
	}

	metrics := defaultMetrics
	if metricParam != "" {
		metrics = strings.Split(metricParam, ",")
	}

	result, err := h.mtsStore.QueryMetrics(r.Context(), metrics, from, to, nodeID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read metrics")

		return
	}

	output := make(map[string][]metricResponse, len(result))
	for metric, points := range result {
		values := make([]metricResponse, 0, len(points))
		for _, p := range points {
			values = append(values, metricResponse{
				NodeID: nodeID, Metric: metric, Value: p.Value,
				Timestamp: p.Timestamp.Unix(),
			})
		}
		output[metric] = values
	}

	JSONResponse(w, http.StatusOK, output)
}
