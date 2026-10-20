package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
)

// registerAgentEnrollmentProtocolRoutes 注册不依赖管理会话的入网协议端点。
// 这些端点由授权秘密或请求令牌本人认证，因此仍是公开路由。
func (r *Router) registerAgentEnrollmentProtocolRoutes(api *http.ServeMux) {
	discovery := handler.NewAgentDiscoveryHandler(r.enrollment, r.agentDiscoveryOrigin, false)
	discovery.SetEnrollmentMetrics(r.enrollmentMetrics)
	api.Handle("POST /api/v1/agent-enrollment/challenge",
		r.observeEnrollment(enrollmentFamilyChallenge,
			enrollmentPrivate(r.enrollmentTransport(http.HandlerFunc(discovery.HandleChallenge)))))
	api.Handle("POST /api/v1/agent-enrollment/requests",
		r.observeEnrollment(enrollmentFamilyRequest,
			enrollmentPrivate(r.enrollmentTransport(http.HandlerFunc(discovery.HandleCreateRequest)))))
	api.Handle("POST /api/v1/agent-enrollment/requests/{id}/status",
		r.observeEnrollment(enrollmentFamilyPoll,
			enrollmentPrivate(r.enrollmentTransport(http.HandlerFunc(discovery.HandleRequestStatus)))))
	api.Handle("POST /api/v1/agent-enrollment/requests/{id}/claim",
		r.observeEnrollment(enrollmentFamilyClaim,
			enrollmentPrivate(r.enrollmentTransport(http.HandlerFunc(discovery.HandleClaim)))))
}

// agentDiscoveryOrigin 给出签名绑定的规范来源：优先使用部署配置的外部地址。
func (r *Router) agentDiscoveryOrigin(request *http.Request) string {
	if base := strings.TrimSpace(r.deploymentAccess.PublicBaseURL()); base != "" {
		return base
	}
	info := middleware.RequestInfoFrom(request)
	return info.Scheme + "://" + info.Host
}

// enrollmentTransport 要求入网流量走 TLS（含受信任代理带来的 https），
// 只有目标 Host 是本机回环地址的显式开发访问才允许明文 HTTP。
func (r *Router) enrollmentTransport(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !middleware.IsSecure(request) && !loopbackRequestHost(request) {
			handler.JSONError(w, http.StatusBadRequest, "enrollment requires https")
			return
		}
		next.ServeHTTP(w, request)
	})
}

// loopbackRequestHost 判断请求 Host 是否指向本机回环地址。
func loopbackRequestHost(request *http.Request) bool {
	host := strings.TrimSpace(request.Host)
	if host == "" {
		return false
	}
	if parsed, err := netip.ParseAddrPort(host); err == nil {
		return parsed.Addr().Unmap().IsLoopback()
	}
	name := host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		name = parsedHost
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(strings.Trim(name, "[]"))
	return err == nil && address.Unmap().IsLoopback()
}
