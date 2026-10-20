package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// metadataAdmin 把认证后的主体绑定到SQLite事务；setup权限仍在事务内复查。
func (router *Router) metadataAdmin(next http.Handler, recent bool) http.Handler {
	wrapped := router.admin(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, exists := middleware.AdminPrincipal(request.Context())
		actor := store.MetadataActor{DashboardActor: store.DashboardActor{RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent()}, Bootstrap: !exists}
		if recent && !exists {
			middleware.WriteAuthError(w, http.StatusForbidden, "administrator session is required")
			return
		}
		if exists {
			if principal.User.Role != model.AdminRoleOwner && principal.User.Role != model.AdminRoleAdmin {
				middleware.WriteAuthError(w, http.StatusForbidden, "administrator permission is required")
				return
			}
			now := model.NowUTC()
			if router.security != nil {
				now = router.security.Now()
			}
			if recent && !principal.Session.RecentlyAuthenticated(now) {
				middleware.WriteAuthError(w, http.StatusPreconditionRequired, "recent authentication is required")
				return
			}
			actor.UserID, actor.SessionID = principal.User.ID, principal.Session.ID
		}
		next.ServeHTTP(w, request.WithContext(store.WithMetadataActor(request.Context(), actor)))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		wrapped.ServeHTTP(w, r)
	})
}

// metadataOwner 在要求 owner 角色与最近认证之后，把已认证主体绑定到元数据审计上下文。
//
// metadataAdmin 只允许 owner/admin，会写入审计；owner 专属路由需要同一份审计主体，
// 因此把“注入审计主体”拆出来复用，避免两套授权逻辑漂移。
func (router *Router) metadataOwner(next http.Handler) http.Handler {
	return router.recentAuthOwnerAdmin(router.metadataActor(next))
}

// metadataActor 只把已认证主体绑定到审计上下文；角色与最近认证由外层中间件决定。
func (router *Router) metadataActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, exists := middleware.AdminPrincipal(request.Context())
		if !exists {
			middleware.WriteAuthError(w, http.StatusForbidden, "administrator session is required")
			return
		}
		actor := store.MetadataActor{DashboardActor: store.DashboardActor{
			UserID: principal.User.ID, SessionID: principal.Session.ID,
			RequestID: middleware.RequestID(request.Context()),
			IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent()}}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, request.WithContext(store.WithMetadataActor(request.Context(), actor)))
	})
}

type metadataRoutePolicy struct{ action, kind, id string }

var metadataPolicies = map[string]metadataRoutePolicy{
	"GET /api/v1/ssh-keys/{id}":            {"ssh_key.reveal", "ssh_key", ""},
	"POST /api/v1/ssh-keys":                {"ssh_key.import", "ssh_key", ""},
	"POST /api/v1/ssh-keys/generate":       {"ssh_key.generate", "ssh_key", ""},
	"DELETE /api/v1/ssh-keys/{id}":         {"ssh_key.delete", "ssh_key", ""},
	"POST /api/v1/nodes":                   {"node.create", "node", ""},
	"PUT /api/v1/nodes/{id}":               {"node.update", "node", ""},
	"PUT /api/v1/nodes/sort":               {"node.sort", "node_group", ""},
	"POST /api/v1/nodes/{id}/token/rotate": {"node.token.rotate", "node", ""},
	"POST /api/v1/nodes/{id}/token/revoke": {"node.token.revoke", "node", ""},
	"POST /api/v1/groups":                  {"group.create", "group", ""},
	"PUT /api/v1/groups/{id}":              {"group.update", "group", ""},
	"DELETE /api/v1/groups/{id}":           {"group.delete", "group", ""},
	"PUT /api/v1/groups/{id}/default":      {"group.default", "group", ""},
	"PUT /api/v1/groups/sort":              {"group.sort", "group_order", "all"},
	"PUT /api/v1/settings/site":            {"site_settings.update", "site_settings", "global"},
}

func metadataAuditPolicy(request *http.Request) (action, kind, id string) {
	policy, exists := metadataPolicies[request.Pattern]
	if !exists {
		return "", "", ""
	}
	id = policy.id
	if id == "" {
		id = request.PathValue("id")
	}
	return policy.action, policy.kind, id
}
