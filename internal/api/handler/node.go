package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
)

type NodeHandler struct {
	accelerators    AcceleratorService
	lifecycleViews  NodeLifecycleViews
	locationViews   NodeLocationViews
	nodeStore       *store.NodeStore
	erasure         *store.MetricErasureStore
	mtsStore        *store.MTSStore
	settingsStore   *store.SiteSettingsStore
	taskEnqueuer    AgentTaskEnqueuer
	agentHub        *service.AgentHub
	customMetrics   *store.CustomMetricStore
	metricCatalog   *store.MetricCatalogStore
	queryBudget     *metricquery.Service
	ingestBudget    agentIngestBudget
	trafficResolver *store.TrafficAccountingResolver
	// publicSnapshot 复用指标流生产者的公共节点快照；nil 时始终直连查询。
	publicSnapshot publicSnapshotSource
	// historyShare 合并在途的相同公共历史读取；nil 时逐次直读。
	historyShare *metricHistoryShare
}

// WithPublicSnapshot 注入公共快照来源；未注入或快照过期时保持直连查询。
func (h *NodeHandler) WithPublicSnapshot(source publicSnapshotSource) *NodeHandler {
	h.publicSnapshot = source
	return h
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
	handler := &NodeHandler{nodeStore: nodeStore, mtsStore: mtsStore, historyShare: newHistoryShare[map[string][]store.TimePoint]()}
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

func (h *NodeHandler) WithCustomMetrics(store *store.CustomMetricStore) *NodeHandler {
	h.customMetrics = store
	return h
}

// WithTrafficResolver 注入校准感知的周期流量解析器；未注入时退回原始 MTS 聚合。
func (h *NodeHandler) WithTrafficResolver(resolver *store.TrafficAccountingResolver) *NodeHandler {
	h.trafficResolver = resolver
	return h
}

func (h *NodeHandler) WithMetricCatalog(catalog *store.MetricCatalogStore) *NodeHandler {
	h.metricCatalog = catalog
	return h
}

func (h *NodeHandler) WithMetricQueryBudget(queries *metricquery.Service) *NodeHandler {
	h.queryBudget = queries
	return h
}

func (h *NodeHandler) HandleListNodes(w http.ResponseWriter, r *http.Request) {
	groupID := r.URL.Query().Get("group_id")
	settings, err := loadSiteSettings(r.Context(), h.settingsStore)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load site settings")
		return
	}
	if snapshot, ok := h.cachedPublicSnapshot(settings.ShowAccelerators); ok {
		if writeConditionalNodes(w, r, snapshot.Generation) {
			return
		}
		JSONResponse(w, http.StatusOK, filterPublicNodes(snapshot.Nodes, groupID, model.NowUTC()))
		return
	}

	nodes, err := h.nodeStore.ListPublicNodes(r.Context(), groupID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list nodes")

		return
	}

	output := make([]publicNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		response, err := h.buildPublicNodeResponse(r.Context(), publicNode(node, settings), settings)
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
	settings, err := loadSiteSettings(r.Context(), h.settingsStore)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load site settings")
		return
	}
	if snapshot, ok := h.cachedPublicSnapshot(settings.ShowAccelerators); ok {
		response, found := findPublicNode(snapshot.Nodes, id)
		if !found {
			JSONError(w, http.StatusNotFound, "node not found")
			return
		}
		if writeConditionalNodes(w, r, snapshot.Generation) {
			return
		}
		JSONResponse(w, http.StatusOK, response)
		return
	}

	node, err := h.nodeStore.GetPublicNode(r.Context(), id)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get node")

		return
	}

	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")

		return
	}
	response, err := h.buildPublicNodeResponse(r.Context(), publicNode(*node, settings), settings)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load node metrics")
		return
	}
	JSONResponse(w, http.StatusOK, response)
}

func (h *NodeHandler) HandleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body nodeUpdateRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)

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
	if respondMetadataMutationError(w, err) {
		return
	}
	if respondAlertScopeCapacity(w, err) {
		return
	}
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

// WithErasureStore 注入耐久擦除账本；删除节点因此变成可重试的异步生命周期请求。
func (h *NodeHandler) WithErasureStore(jobs *store.MetricErasureStore) *NodeHandler {
	h.erasure = jobs
	return h
}

func (h *NodeHandler) HandleDeleteNode(w http.ResponseWriter, r *http.Request) {
	view, replayed, ok := createDeletionJob(w, r, h.erasure, model.MetricErasureDeleteNode,
		r.PathValue("id"), model.NowUTC())
	if !ok {
		return
	}
	JSONResponse(w, http.StatusAccepted, deletionAcceptedResponse{Job: view, Replayed: replayed})
}

func (h *NodeHandler) HandleNodeReport(w http.ResponseWriter, r *http.Request) {
	h.handleMetricReport(w, r)
}

// HandleGetNodeCustomMetrics returns the enabled custom metrics bound to a node.
func (h *NodeHandler) HandleGetNodeCustomMetrics(w http.ResponseWriter, r *http.Request) {
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
	if h.customMetrics == nil {
		JSONError(w, http.StatusServiceUnavailable, "custom metrics are unavailable")
		return
	}
	metrics, err := h.customMetrics.ListEnabledForNode(r.Context(), nodeID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list custom metrics")
		return
	}
	var allowed map[string]bool
	if h.metricCatalog != nil {
		allowed, err = h.metricCatalog.PublicKeys(r.Context())
		if err != nil {
			JSONError(w, http.StatusServiceUnavailable, "metric catalog is unavailable")
			return
		}
	}
	response := make([]customMetricNodeResponse, 0, len(metrics))
	for _, metric := range metrics {
		if allowed != nil && !allowed[model.CustomMetricPrefix+metric.Name] {
			continue
		}
		response = append(response, customMetricNodeResponse{
			ID:              metric.ID,
			Name:            metric.Name,
			IntervalSeconds: metric.IntervalSeconds,
		})
	}
	JSONResponse(w, http.StatusOK, response)
}

type customMetricNodeResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	IntervalSeconds int    `json:"interval_seconds"`
}
