package handler

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type MetricsHandler struct {
	nodeStore     *store.NodeStore
	mtsStore      *store.MTSStore
	settingsStore *store.SiteSettingsStore
}

func NewMetricsHandler(
	nodeStore *store.NodeStore,
	mtsStore *store.MTSStore,
	settingsStores ...*store.SiteSettingsStore,
) *MetricsHandler {
	handler := &MetricsHandler{nodeStore: nodeStore, mtsStore: mtsStore}
	if len(settingsStores) > 0 {
		handler.settingsStore = settingsStores[0]
	}
	return handler
}

type metricsSnapshot struct {
	Timestamp string         `json:"timestamp"`
	Nodes     []nodeResponse `json:"nodes"`
}

func (h *MetricsHandler) HandleMetricsWS(w http.ResponseWriter, r *http.Request) {
	handler := websocket.Handler(func(conn *websocket.Conn) {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			data, err := h.snapshot(r.Context())
			if err != nil {
				return
			}
			if err := websocket.JSON.Send(conn, data); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})

	handler.ServeHTTP(w, r)
}

func (h *MetricsHandler) snapshot(ctx context.Context) (metricsSnapshot, error) {
	snapshot := metricsSnapshot{Timestamp: model.NowUTC().Format(time.RFC3339), Nodes: []nodeResponse{}}
	if h.nodeStore == nil || h.mtsStore == nil {
		return snapshot, nil
	}
	nodes, err := h.nodeStore.ListPublicNodes(ctx, "")
	if err != nil {
		return metricsSnapshot{}, err
	}
	settings, err := loadSiteSettings(ctx, h.settingsStore)
	if err != nil {
		return metricsSnapshot{}, err
	}
	for _, node := range nodes {
		response, err := buildNodeResponse(ctx, publicNode(node, settings), h.mtsStore)
		if err != nil {
			return metricsSnapshot{}, err
		}
		snapshot.Nodes = append(snapshot.Nodes, response)
	}
	return snapshot, nil
}
