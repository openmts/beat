package serviceprincipalhttp

import (
	"net/http"

	"github.com/beat/backend/internal/serviceprincipal"
)

// Protection 注入真实身份上下文；缺失的保护器会拒绝其路由。
type Protection struct {
	OwnerRead  func(http.Handler) http.Handler
	OwnerWrite func(http.Handler) http.Handler
}

type access uint8

const (
	ownerRead access = iota
	ownerWrite
)

type route struct {
	pattern string
	access  access
	handler http.HandlerFunc
}

// RegisterProtected 用中央 Router 的身份保护器注册管理路由。
func (h *Handler) RegisterProtected(mux *http.ServeMux, protection Protection) {
	for _, entry := range h.routes() {
		mux.Handle(entry.pattern, protection.wrap(entry.access, entry.handler))
	}
}

func (p Protection) wrap(level access, next http.Handler) http.Handler {
	guard := p.OwnerRead
	if level == ownerWrite {
		guard = p.OwnerWrite
	}
	if guard == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { respond(w, nil, serviceprincipal.ErrStorage) })
	}
	return guard(next)
}

func (h *Handler) routes() []route {
	return []route{
		{"GET /api/v1/admin/service-tokens", ownerRead, h.list},
		{"POST /api/v1/admin/service-tokens", ownerWrite, h.create},
		{"POST /api/v1/admin/service-tokens/{token_id}/rotate", ownerWrite, h.rotate},
		{"POST /api/v1/admin/service-tokens/{token_id}/revoke", ownerWrite, h.revoke},
	}
}
