package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// publicNetworkQualityReuseWindow 是复用上一代公共网络质量视图的时间上限。探测结果按任务
// 间隔落库，窗口只用于吸收同一时刻到达的观看者，因此取一个短窗口；超过窗口、或任务与探测
// 结果被写入之后，下一个请求重建视图，绝不把上一代结果继续当作当前探测值。
const publicNetworkQualityReuseWindow = 5 * time.Second

// networkQualityShare 合并在途的公共网络质量重建：同时到达的观看者共享一次构建。
type networkQualityShare = historyShare[networkQualityView]

// networkQualityView 是一次构建结果：成功时带视图与快照，失败时只带对外文案。
type networkQualityView struct {
	snapshot networkQualitySnapshot
	failure  string
}

// networkQualitySnapshot 是最近一次成功构建的公共网络质量视图。Views 只读共享，调用方
// 不做就地修改。
type networkQualitySnapshot struct {
	Generation uint64
	Timestamp  time.Time
	Views      []networkTaskView
}

// networkQualityETag 由视图代生成：同一代内响应字节相同，条件 GET 可直接命中 304。
func networkQualityETag(generation uint64) string {
	return fmt.Sprintf(`W/"public-network-quality-%d"`, generation)
}

// invalidateNetworkQuality 让当前视图失效：任务、分配或探测结果一旦写入，下一个请求必须
// 重新构建，而不是继续复用上一代结果。
func (h *NetworkHandler) invalidateNetworkQuality() {
	h.qualityGeneration.Add(1)
}

// cachedNetworkQuality 读取同代且在复用窗口内的视图；过期、失效或尚未构建时 ok=false。
func (h *NetworkHandler) cachedNetworkQuality() (networkQualitySnapshot, bool) {
	value := h.quality.Load()
	if value == nil || value.Timestamp.IsZero() {
		return networkQualitySnapshot{}, false
	}
	if value.Generation != h.qualityGeneration.Load() {
		return networkQualitySnapshot{}, false
	}
	if h.now().Sub(value.Timestamp) > publicNetworkQualityReuseWindow {
		return networkQualitySnapshot{}, false
	}
	return *value, true
}

// storeNetworkQualityView 记录一次成功构建并推进视图代；失败构建不会走到这里，因此上一份
// 可用视图会一直保留到下一次成功构建。
func (h *NetworkHandler) storeNetworkQualityView(views []networkTaskView) networkQualitySnapshot {
	snapshot := networkQualitySnapshot{
		Generation: h.qualityGeneration.Add(1),
		Timestamp:  h.now(),
		Views:      views,
	}
	h.quality.Store(&snapshot)
	return snapshot
}

// buildPublicQualityView 构建公共网络质量视图；失败时返回固定的对外文案，不泄露存储细节。
func (h *NetworkHandler) buildPublicQualityView(ctx context.Context) networkQualityView {
	tasks, err := h.tasks.ListTasks(ctx, true)
	if err != nil {
		return networkQualityView{failure: "failed to list network quality"}
	}
	views, err := h.buildTaskViews(ctx, tasks, true)
	if err != nil {
		return networkQualityView{failure: "failed to load network quality"}
	}
	return networkQualityView{snapshot: h.storeNetworkQualityView(views)}
}

// sharedPublicQualityView 复用仍有效的视图；否则合并并发重建，只有 leader 记录新视图。
func (h *NetworkHandler) sharedPublicQualityView(ctx context.Context) (networkQualityView, error) {
	if snapshot, ok := h.cachedNetworkQuality(); ok {
		return networkQualityView{snapshot: snapshot}, nil
	}
	return h.qualityShare.do(ctx, "public-network-quality", func(readCtx context.Context) (networkQualityView, error) {
		if snapshot, ok := h.cachedNetworkQuality(); ok {
			return networkQualityView{snapshot: snapshot}, nil
		}
		return h.buildPublicQualityView(readCtx), nil
	})
}

// writeNetworkQualityView 设置校验器并在命中 If-None-Match 时结束响应，否则写出视图。
func writeNetworkQualityView(w http.ResponseWriter, r *http.Request, snapshot networkQualitySnapshot) {
	etag := networkQualityETag(snapshot.Generation)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	JSONResponse(w, http.StatusOK, snapshot.Views)
}
