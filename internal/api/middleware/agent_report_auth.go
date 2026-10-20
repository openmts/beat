package middleware

import (
	"context"
	"net/http"
)

type agentReportAuthenticationKey struct{}

// AgentReportAuth 在准入前只读认证，token 最近使用时间随 contact 事务提交。
func AgentReportAuth(authenticator AgentAuthenticator, legacyToken string, resolver LegacyNodeNameResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		authenticated := AgentAuth(authenticator, legacyToken, resolver)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), agentReportAuthenticationKey{}, true)
			authenticated.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
