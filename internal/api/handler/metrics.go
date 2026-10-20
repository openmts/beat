package handler

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type MetricsHandler struct {
	stream         http.Handler
	accelerators   AcceleratorService
	lifecycleViews NodeLifecycleViews
	locationViews  NodeLocationViews
	nodeStore      *store.NodeStore
	mtsStore       *store.MTSStore
	settingsStore  *store.SiteSettingsStore
	metricCatalog  *store.MetricCatalogStore
	// latest 与 generation 保存最近一次成功的公共快照；由生产者整体替换。
	latest     atomic.Pointer[publicSnapshot]
	generation atomic.Uint64
}

func (h *MetricsHandler) WithMetricCatalog(catalog *store.MetricCatalogStore) *MetricsHandler {
	h.metricCatalog = catalog
	return h
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
	at                  time.Time
	Timestamp           string               `json:"timestamp"`
	Nodes               []publicNodeResponse `json:"nodes"`
	AcceleratorsVisible bool                 `json:"-"`
}

func (h *MetricsHandler) WithMetricsStream(stream http.Handler) *MetricsHandler {
	h.stream = stream
	return h
}

func (h *MetricsHandler) HandleMetricsWS(w http.ResponseWriter, r *http.Request) {
	if h.stream == nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "metrics stream unavailable: disabled", http.StatusServiceUnavailable)
		return
	}
	h.stream.ServeHTTP(w, r)
}

// MetricsOriginPolicy 返回公共指标流的来源判定。同源页面始终放行：浏览器自己
// 页面的来源无法被第三方伪造，页面也可能通过局域网地址、回环地址或公网域名访问；
// 配置了外部来源时该列表是额外白名单，用于上游代理改写 Host 的部署。
// 缺少来源只对无凭据的非浏览器客户端开放。
func MetricsOriginPolicy(policy deploymentaccess.OriginPolicy) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin, err := websocket.Origin(&websocket.Config{Version: 13}, r)
		if err != nil {
			return false
		}
		if origin == nil || policy.Wildcard() {
			return true
		}
		return sameOriginOrigin(origin, r.Host) || policy.Allows(origin.String())
	}
}

// sameOriginOrigin 判定浏览器来源是否就是请求自身来源。比较时归一化大小写与
// 默认端口（http/80、https/443），避免上游代理补写或省略端口时把同源误判为跨站。
func sameOriginOrigin(origin *url.URL, host string) bool {
	if origin == nil || strings.TrimSpace(origin.Scheme) == "" {
		return false
	}
	expected := canonicalAuthority(origin.Scheme, host)
	return expected != "" && expected == canonicalAuthority(origin.Scheme, origin.Host)
}

// canonicalAuthority 把 authority 归一化成小写 host[:port]，并去掉该 scheme 的默认端口。
func canonicalAuthority(scheme, authority string) string {
	parsed, err := url.Parse("//" + strings.TrimSpace(authority))
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	port := parsed.Port()
	if port == defaultPort(scheme) {
		port = ""
	}
	if port == "" {
		return strings.ToLower(parsed.Hostname())
	}
	return strings.ToLower(net.JoinHostPort(parsed.Hostname(), port))
}

// defaultPort 返回该 scheme 的默认端口；未知 scheme 返回空串表示没有默认端口。
func defaultPort(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (h *MetricsHandler) BuildMetricsSnapshot(ctx context.Context) (metricsfanout.Snapshot, error) {
	value, err := h.snapshot(ctx)
	if err != nil {
		return metricsfanout.Snapshot{}, err
	}
	return metricsfanout.Snapshot{Value: value, Timestamp: value.at}, nil
}

func (h *MetricsHandler) snapshot(ctx context.Context) (metricsSnapshot, error) {
	at := model.NowUTC().Truncate(time.Second)
	snapshot := metricsSnapshot{at: at, Timestamp: at.Format(time.RFC3339), Nodes: []publicNodeResponse{}}
	if h.nodeStore == nil || h.mtsStore == nil {
		return snapshot, errors.New("metrics storage unavailable")
	}
	if healthy, _ := h.mtsStore.Health(); !healthy {
		return snapshot, ErrPublicMetricQuery
	}
	nodes, err := h.nodeStore.ListPublicNodes(ctx, "")
	if err != nil {
		return metricsSnapshot{}, err
	}
	settings, err := loadSiteSettings(ctx, h.settingsStore)
	if err != nil {
		return metricsSnapshot{}, err
	}
	snapshot.AcceleratorsVisible = settings.ShowAccelerators
	for _, node := range nodes {
		response, err := NewNodeHandler(h.nodeStore, h.mtsStore).WithMetricCatalog(h.metricCatalog).WithLocationViews(h.locationViews).WithLifecycleViews(h.lifecycleViews).WithAccelerators(h.accelerators).buildPublicNodeResponse(ctx, publicNode(node, settings), settings)
		if err != nil {
			return metricsSnapshot{}, err
		}
		snapshot.Nodes = append(snapshot.Nodes, response)
	}
	if healthy, _ := h.mtsStore.Health(); !healthy {
		return metricsSnapshot{}, ErrPublicMetricQuery
	}
	h.storePublicSnapshot(snapshot)
	return snapshot, nil
}
