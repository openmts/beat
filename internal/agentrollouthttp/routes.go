package agentrollouthttp

import (
	"net/http"

	"github.com/beat/backend/internal/agentrollout"
)

type Protection struct {
	OwnerRead  func(http.Handler) http.Handler
	OwnerWrite func(http.Handler) http.Handler
	Agent      func(http.Handler) http.Handler
}

type access uint8

const (
	ownerRead access = iota
	ownerWrite
	agentAccess
)

type route struct {
	pattern string
	access  access
	handler http.HandlerFunc
}

// Register保留注入真实身份上下文的独立入口；中央Router使用RegisterProtected。
func (h *Handler) Register(mux *http.ServeMux) {
	for _, route := range h.routes() {
		mux.HandleFunc(route.pattern, route.handler)
	}
}

// 任一缺失的保护器都拒绝其路由，不能意外降级为无认证注册。
func (h *Handler) RegisterProtected(mux *http.ServeMux, protection Protection) {
	for _, route := range h.routes() {
		mux.Handle(route.pattern, protection.wrap(route.access, route.handler))
	}
}

func (p Protection) wrap(access access, next http.Handler) http.Handler {
	var guard func(http.Handler) http.Handler
	switch access {
	case ownerRead:
		guard = p.OwnerRead
	case ownerWrite:
		guard = p.OwnerWrite
	case agentAccess:
		guard = p.Agent
	}
	if guard == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { respond(w, nil, agentrollout.ErrUnavailable) })
	}
	return guard(next)
}

func (h *Handler) routes() []route {
	routes := []route{
		{"GET /api/v1/admin/agent-rollout-status", ownerRead, h.status},
		{"GET /api/v1/admin/agent-releases", ownerRead, h.listReleases},
		{"GET /api/v1/admin/agent-releases/{release_id}", ownerRead, h.release},
		{"POST /api/v1/admin/agent-releases/import", ownerWrite, h.importManifest},
		{"PUT /api/v1/admin/agent-releases/{release_id}/artifacts/{artifact_id}", ownerWrite, h.upload},
		{"POST /api/v1/admin/agent-releases/{release_id}/revoke", ownerWrite, h.revoke},
		{"DELETE /api/v1/admin/agent-releases/{release_id}", ownerWrite, h.deleteRelease},
		{"GET /api/v1/admin/agent-rollouts", ownerRead, h.listRollouts},
		{"POST /api/v1/admin/agent-rollouts", ownerWrite, h.create},
		{"GET /api/v1/admin/agent-rollouts/{rollout_id}", ownerRead, h.rollout},
		{"GET /api/v1/admin/agent-rollouts/{rollout_id}/targets", ownerRead, h.targets},
		{"DELETE /api/v1/admin/agent-rollouts/{rollout_id}", ownerWrite, h.deleteRollout},
		{"POST /api/v1/agents/rollout/lease", agentAccess, h.lease},
		{"POST /api/v1/agents/rollout/observations", agentAccess, h.observe},
		{"GET /api/v1/agents/rollout/artifacts/{assignment_id}/{artifact_id}", agentAccess, h.download},
	}
	for _, action := range []string{"promote", "pause", "resume", "abort"} {
		routes = append(routes, route{"POST /api/v1/admin/agent-rollouts/{rollout_id}/" + action, ownerWrite, h.control(action)})
	}
	return routes
}
