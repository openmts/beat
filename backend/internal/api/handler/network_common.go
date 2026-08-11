package handler

import (
	"errors"
	"net/http"
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
	tasks *store.NetworkTaskStore
	nodes *store.NodeStore
	mts   *store.MTSStore
	now   func() time.Time
}

type networkTaskRequest struct {
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	Target              string   `json:"target"`
	IPFamily            string   `json:"ip_family"`
	IntervalSeconds     int      `json:"interval_seconds"`
	TimeoutMilliseconds int      `json:"timeout_milliseconds"`
	AllNodes            bool     `json:"all_nodes"`
	Enabled             bool     `json:"enabled"`
	IsPublic            bool     `json:"is_public"`
	SortOrder           int      `json:"sort_order"`
	NodeIDs             []string `json:"node_ids"`
}

type networkTaskView struct {
	Task  model.NetworkTask  `json:"task"`
	Nodes []networkNodeState `json:"nodes"`
}

type networkNodeState struct {
	Node   model.NetworkNode         `json:"node"`
	Latest *store.NetworkProbeLatest `json:"latest"`
}

type networkHistoryResponse struct {
	TaskID string                    `json:"task_id"`
	NodeID string                    `json:"node_id"`
	From   time.Time                 `json:"from"`
	To     time.Time                 `json:"to"`
	Points []store.NetworkProbePoint `json:"points"`
}

func NewNetworkHandler(
	tasks *store.NetworkTaskStore,
	nodes *store.NodeStore,
	mtsStore *store.MTSStore,
) *NetworkHandler {
	return &NetworkHandler{tasks: tasks, nodes: nodes, mts: mtsStore, now: model.NowUTC}
}

func (body networkTaskRequest) task() model.NetworkTask {
	return model.NetworkTask{
		Name: body.Name, Type: body.Type, Target: body.Target, IPFamily: body.IPFamily,
		IntervalSeconds: body.IntervalSeconds, TimeoutMilliseconds: body.TimeoutMilliseconds,
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
