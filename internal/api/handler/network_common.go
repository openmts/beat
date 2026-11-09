package handler

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const (
	networkAssignmentTTL = 90 * time.Second
	maxNetworkRange      = 31 * 24 * time.Hour
	defaultNetworkRange  = 24 * time.Hour
	maxNetworkResultBody = 256 * 1024
)

type NetworkHandler struct {
	tasks   *store.NetworkTaskStore
	nodes   *store.NodeStore
	mts     *store.MTSStore
	erasure *store.MetricErasureStore
	now     func() time.Time
	// historyShare 合并在途的相同公共网络历史读取；nil 时逐次直读。
	historyShare *historyShare[[]networkHistorySeries]
	// quality 保存最近一次成功的公共网络质量视图；由请求线程整体替换，调用方只读。
	quality atomic.Pointer[networkQualitySnapshot]
	// qualityGeneration 是视图代：任务、分配或探测结果写入后自增，使旧视图立即失效。
	qualityGeneration atomic.Uint64
	// qualityShare 合并在途的公共网络质量重建，避免同时到达的观看者各建一份视图。
	qualityShare *networkQualityShare
}

type networkTaskRequest struct {
	Name                string                `json:"name"`
	Direction           string                `json:"direction"`
	Type                string                `json:"type"`
	TargetMode          string                `json:"target_mode"`
	Target              string                `json:"target"`
	Targets             []model.NetworkTarget `json:"targets"`
	IPFamily            string                `json:"ip_family"`
	IntervalSeconds     int                   `json:"interval_seconds"`
	TimeoutMilliseconds int                   `json:"timeout_milliseconds"`
	PacketCount         int                   `json:"packet_count"`
	AllNodes            bool                  `json:"all_nodes"`
	Enabled             bool                  `json:"enabled"`
	IsPublic            bool                  `json:"is_public"`
	SortOrder           int                   `json:"sort_order"`
	NodeIDs             []string              `json:"node_ids"`
}

type networkTaskView struct {
	Task  model.NetworkTask  `json:"task"`
	Nodes []networkNodeState `json:"nodes"`
}

type networkNodeState struct {
	Node    model.NetworkNode         `json:"node"`
	Latest  *store.NetworkProbeLatest `json:"latest"`
	Targets []networkTargetState      `json:"targets"`
}

type networkTargetState struct {
	TargetID string                    `json:"target_id"`
	Latest   *store.NetworkProbeLatest `json:"latest"`
}

type networkHistorySeries struct {
	TargetID       string                    `json:"target_id"`
	Label          string                    `json:"label"`
	Carrier        string                    `json:"carrier"`
	Region         string                    `json:"region"`
	VisibleAddress bool                      `json:"visible_address"`
	Target         string                    `json:"target,omitempty"`
	Points         []store.NetworkProbePoint `json:"points"`
}

type networkHistoryResponse struct {
	TaskID string                 `json:"task_id"`
	NodeID string                 `json:"node_id"`
	From   time.Time              `json:"from"`
	To     time.Time              `json:"to"`
	Series []networkHistorySeries `json:"series"`
}

func NewNetworkHandler(
	tasks *store.NetworkTaskStore,
	nodes *store.NodeStore,
	mtsStore *store.MTSStore,
) *NetworkHandler {
	return &NetworkHandler{tasks: tasks, nodes: nodes, mts: mtsStore, now: model.NowUTC,
		historyShare: newHistoryShare[[]networkHistorySeries](),
		qualityShare: newHistoryShare[networkQualityView]()}
}

func (body networkTaskRequest) task() model.NetworkTask {
	return model.NetworkTask{
		Name: body.Name, Direction: body.Direction, Type: body.Type, TargetMode: body.TargetMode,
		Target: body.Target, Targets: body.Targets,
		IPFamily: body.IPFamily, IntervalSeconds: body.IntervalSeconds,
		TimeoutMilliseconds: body.TimeoutMilliseconds, PacketCount: body.PacketCount,
		AllNodes: body.AllNodes, Enabled: body.Enabled, IsPublic: body.IsPublic, SortOrder: body.SortOrder,
	}
}

func parseNetworkRange(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	fromText := r.URL.Query().Get("from")
	toText := r.URL.Query().Get("to")
	if fromText == "" && toText == "" {
		return now.Add(-defaultNetworkRange), now, nil
	}
	if fromText == "" || toText == "" {
		return time.Time{}, time.Time{}, errors.New("from and to must be provided together")
	}
	from, err := time.Parse(time.RFC3339, fromText)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid from timestamp")
	}
	to, err := time.Parse(time.RFC3339, toText)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid to timestamp")
	}
	if !from.Before(to) || to.Sub(from) > maxNetworkRange {
		return time.Time{}, time.Time{}, errors.New("network history range is invalid")
	}
	return from.UTC(), to.UTC(), nil
}
