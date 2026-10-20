package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/beat/backend/internal/metricsfanout"
	"github.com/beat/backend/internal/model"
)

// publicSnapshotReuseWindow 是复用上一代快照的时间上限：两个生产周期加调度余量。
// 生产者会跳过构建期间到达的 tick，所以一次「构建 + 调度」最坏跨两个周期；
// 窗口取两倍 cadence 才能覆盖慢实例（构建接近一个 cadence）而不回退到直连查询，
// 同时仍把公共视图的陈旧度限制在十几秒内。超过该窗口仍回退直连查询，
// 绝不把过期数据继续当作当前值。
const publicSnapshotReuseWindow = 2*metricsfanout.Cadence + time.Second

// publicSnapshot 是 runtime-resilience 生产者构建的最近一次公共节点视图。
// 它与指标流共用同一份数据，避免公共 REST 每次请求都重新查询每个节点的 MTS。
type publicSnapshot struct {
	Generation          uint64
	Timestamp           time.Time
	Nodes               []publicNodeResponse
	AcceleratorsVisible bool
}

// publicSnapshotSource 只暴露只读快照；消费方永远不触发新的 MTS 查询。
type publicSnapshotSource interface {
	FreshPublicNodes() (publicSnapshot, bool)
}

// FreshPublicNodes 返回陈旧窗口内的公共节点快照；过期或未构建时 ok=false。
// 返回的切片由生产者整体替换，调用方只读，不做就地修改。
func (h *MetricsHandler) FreshPublicNodes() (publicSnapshot, bool) {
	value := h.latest.Load()
	if value == nil || value.Timestamp.IsZero() {
		return publicSnapshot{}, false
	}
	if age := model.NowUTC().Sub(value.Timestamp); age > publicSnapshotReuseWindow {
		return publicSnapshot{}, false
	}
	return *value, true
}

// storePublicSnapshot 记录一次成功构建；失败构建保留上一份可用快照。
// 新鲜度以「构建完成」时刻计时：value.at 是构建开始时间，慢实例上构建本身
// 可能花掉数秒，若按开始时间计时，快照一发布就接近过期，公共 REST 会退回
// 直连查询（每次四秒以上，还可能撞上查询预算返回 500）。
func (h *MetricsHandler) storePublicSnapshot(value metricsSnapshot) {
	h.latest.Store(&publicSnapshot{
		Generation: h.generation.Add(1), Timestamp: model.NowUTC(), Nodes: value.Nodes,
		AcceleratorsVisible: value.AcceleratorsVisible,
	})
}

// publicNodeETag 由快照代生成：同一代内响应字节相同，条件 GET 可直接命中 304。
func publicNodeETag(generation uint64) string {
	return fmt.Sprintf(`W/"public-nodes-%d"`, generation)
}

// writeConditionalNodes 设置校验器并在命中 If-None-Match 时结束响应。
func writeConditionalNodes(w http.ResponseWriter, r *http.Request, generation uint64) bool {
	etag := publicNodeETag(generation)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") != etag {
		return false
	}
	w.WriteHeader(http.StatusNotModified)
	return true
}

// filterPublicNodes 按分组过滤快照并刷新遥测年龄；分组为空时返回全部公共节点。
// 只复用 MTS 派生值，年龄按当前时间重算，避免把上一代的秒数当作当前值。
func filterPublicNodes(nodes []publicNodeResponse, groupID string, now time.Time) []publicNodeResponse {
	filtered := make([]publicNodeResponse, 0, len(nodes))
	for _, node := range nodes {
		if groupID != "" && node.GroupID != groupID {
			continue
		}
		filtered = append(filtered, refreshTelemetryAge(node, now))
	}
	return filtered
}

// refreshTelemetryAge 用当前时间重算年龄字段；状态阈值以分钟计，不受快照复用窗口影响。
func refreshTelemetryAge(node publicNodeResponse, now time.Time) publicNodeResponse {
	if node.TelemetryAt == nil {
		return node
	}
	seconds := int64(max(now.Sub(*node.TelemetryAt), 0) / time.Second)
	node.TelemetryAgeSeconds = &seconds
	return node
}

func findPublicNode(nodes []publicNodeResponse, id string) (publicNodeResponse, bool) {
	now := model.NowUTC()
	for _, node := range nodes {
		if node.ID == id {
			return refreshTelemetryAge(node, now), true
		}
	}
	return publicNodeResponse{}, false
}

// cachedPublicSnapshot 读取共享快照；未启用、已过期或全局展示开关不匹配时回退到直连路径。
// 开关切换必须先绕过旧快照，否则 ETag 与节点内容会在设置变更后的一个生产周期内保持旧值。
func (h *NodeHandler) cachedPublicSnapshot(acceleratorsVisible bool) (publicSnapshot, bool) {
	if h.publicSnapshot == nil {
		return publicSnapshot{}, false
	}
	value, ok := h.publicSnapshot.FreshPublicNodes()
	if !ok || value.AcceleratorsVisible != acceleratorsVisible {
		return publicSnapshot{}, false
	}
	return value, true
}
