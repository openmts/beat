package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
)

// StatusClientClosedRequest 沿用 nginx 约定：客户端在收到响应前断开连接。
// 它不算服务端错误，不能进 5xx 统计——刷新页面、切换路由与并发取消都会制造这类
// 请求，把它们记成失败会把真实的 5xx 淹没。
const StatusClientClosedRequest = 499

// clientCanceled 报告请求上下文是否已经结束（客户端断开或服务端主动取消）。
// 存储层把这类情况回成 `context canceled`，它不是内部故障。
func clientCanceled(ctx context.Context) bool {
	return ctx.Err() != nil
}

// respondClientCanceled 处理「客户端已经离开」的失败：只留一条可诊断的 Info 日志，
// 不写 WARN，也不制造 500。
func respondClientCanceled(w http.ResponseWriter, r *http.Request, operation string) {
	slog.InfoContext(r.Context(), operation+" canceled by client", "request_id", middleware.RequestID(r.Context()))
	JSONError(w, StatusClientClosedRequest, "client closed request")
}
