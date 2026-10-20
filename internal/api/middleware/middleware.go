package middleware

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/beat/backend/internal/deploymentaccess"
)

// CORS 保留未配置部署访问策略时的历史行为：公共读接口允许任意来源。
func CORS(next http.Handler) http.Handler {
	policy, err := deploymentaccess.ParseOrigins("*")
	if err != nil {
		panic(fmt.Sprintf("wildcard origin policy: %v", err))
	}
	return CORSWithPolicy(policy)(next)
}

// publicInteractionPath 是唯一的公共写端点，只接受有界匿名计数。
const publicInteractionPath = "/api/v1/public/interactions"

// CORSWithPolicy 把部署访问策略应用到公共读路由。
// 只有公共读路径、且来源被策略允许时才写入 CORS 头；凭据头永远不会被设置。
func CORSWithPolicy(policy deploymentaccess.OriginPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicCORSPath(r.URL.Path) {
				applyPublicCORS(w, r, policy)
			}
			if r.Method == http.MethodOptions && publicCORSPath(r.URL.Path) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// applyPublicCORS 只回显被允许的具体来源；通配策略保持 `*` 且不带凭据。
func applyPublicCORS(w http.ResponseWriter, r *http.Request, policy deploymentaccess.OriginPolicy) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if policy.Wildcard() {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if origin != "" && policy.Allows(origin) {
		normalized, err := deploymentaccess.NormalizeOrigin(origin)
		if err == nil {
			w.Header().Set("Access-Control-Allow-Origin", normalized)
			w.Header().Add("Vary", "Origin")
		}
	}
	w.Header().Set("Access-Control-Allow-Methods", allowedPublicMethods(r.URL.Path))
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func allowedPublicMethods(path string) string {
	if path == publicInteractionPath {
		return "POST, OPTIONS"
	}
	return "GET, OPTIONS"
}

func publicCORSPath(path string) bool {
	if path == "/api/v1/settings/site" || path == "/api/v1/groups" || path == "/api/v1/nodes" || path == publicInteractionPath {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/network/quality") {
		return true
	}
	if !strings.HasPrefix(path, "/api/v1/nodes/") {
		return false
	}
	return !strings.Contains(path, "/install") && !strings.Contains(path, "/token/") &&
		path != "/api/v1/nodes/manage" && path != "/api/v1/nodes/report"
}

func ContentTypeJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(content []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(content)
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func Logging(next http.Handler) http.Handler {
	return LoggingWithObserver(next, nil)
}

type HTTPObserver interface {
	ObserveHTTP(method, route string, status int, duration time.Duration)
}

// statusClientClosedRequest 沿用 nginx 约定：客户端在响应送达前断开连接。
// 它不是服务端错误，不能进 5xx 统计。
const statusClientClosedRequest = 499

// reportedStatus 归一化访问日志与 HTTP 指标里的状态码。请求上下文已经结束时，
// 处理器即便写了 500 也没有送达任何客户端：刷新页面、切换路由与并发取消都会制造
// 这类请求，把它们记成服务端错误会把真实的 5xx 淹没在噪音里。真正的失败原因仍由
// 处理器自己的 WARN/ERROR 日志保留。
func reportedStatus(r *http.Request, status int) (int, bool) {
	if r.Context().Err() == nil {
		return status, false
	}
	if status >= http.StatusInternalServerError {
		return statusClientClosedRequest, true
	}
	return status, true
}

func LoggingWithObserver(next http.Handler, observer HTTPObserver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		route := r.Pattern
		status, canceled := reportedStatus(r, rw.statusCode)
		if observer != nil {
			observer.ObserveHTTP(r.Method, route, status, duration)
		}
		attributes := []any{
			"request_id", RequestID(r.Context()), "method", r.Method, "route", route,
			"path", r.URL.Path, "status", status, "duration_ms", duration.Milliseconds(),
			"client_ip", ClientIP(r),
		}
		if canceled {
			attributes = append(attributes, "client_canceled", true, "written_status", rw.statusCode)
		}
		attributes = append(attributes, upgradeAttributes(r)...)
		slog.InfoContext(r.Context(), "http request", attributes...)
	})
}

// upgradeAttributes 给 WebSocket 升级请求补上 Host 与 Origin。握手被拒时这两个值
// 决定是否判为同源，缺了它们运维只能看到 400/403，无法判断原因。
func upgradeAttributes(r *http.Request) []any {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return nil
	}
	return []any{"host", r.Host, "origin", strings.TrimSpace(r.Header.Get("Origin"))}
}

func ObserveStatus(next http.Handler, after func(int)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, request)
		after(rw.statusCode)
	})
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.ErrorContext(r.Context(), "panic recovered", "request_id", RequestID(r.Context()),
					"error", fmt.Sprint(err), "stack", string(debug.Stack()))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				http.Error(w, `{"code": 500, "message": "internal server error"}`, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
