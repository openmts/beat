package main

import (
	"net/http"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/ingress"
)

// initializeIngress 构造带校验的容量配置与限流器；非法配置直接让启动失败。
func initializeIngress(getenv func(string) string) (*ingress.Limiter, error) {
	capacity, err := ingress.FromEnvironment(getenv)
	if err != nil {
		return nil, err
	}
	return ingress.New(capacity)
}

// ingressIdentity 优先使用会话指纹，其次使用可信代理解析出的客户端 IP。
// 原始会话令牌与 IP 不会成为指标标签。
func ingressIdentity(request *http.Request) string {
	if cookie, err := request.Cookie(handler.AdminSessionCookieName); err == nil && cookie.Value != "" {
		return "session:" + middleware.Fingerprint(cookie.Value)
	}
	return "ip:" + middleware.ClientIP(request)
}

// ingressLimiter 在测试装配里可以缺省，此时入口准入不生效。
func ingressLimiter(runtime *runtimeState) *ingress.Limiter {
	if runtime == nil {
		return nil
	}
	return runtime.ingress.Load()
}
