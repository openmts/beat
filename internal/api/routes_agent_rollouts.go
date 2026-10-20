package api

import (
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollouthttp"
	"github.com/beat/backend/internal/agentrolloutremote"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

const rolloutUploadPattern = "PUT /api/v1/admin/agent-releases/{release_id}/artifacts/{artifact_id}"

const rolloutReadTimeout = 30 * time.Second

const rolloutMaximumAuthorizationBytes = 512

type rolloutAdmission struct{ incoming, owners, agents *rolloutBudget }

func WithAgentRollouts(service *agentrollout.Service) RouterOption {
	return func(r *Router) { r.agentRollouts = service }
}

func WithAgentReleaseRemote(service *agentrolloutremote.Service) RouterOption {
	return func(r *Router) { r.agentReleaseRemote = service }
}

func (r *Router) registerAgentRolloutRoutes(mux *http.ServeMux) {
	if r.agentRollouts == nil {
		return
	}
	admission := newRolloutAdmission()
	r.rolloutRequests = admission
	h := agentrollouthttp.NewWithRemote(r.agentRollouts, r.agentReleaseRemote)
	h.RegisterProtected(mux, agentrollouthttp.Protection{
		OwnerRead: func(next http.Handler) http.Handler {
			return admission.incomingGuard(r.rolloutOwner(admission.identityGuard(next, false), false))
		},
		OwnerWrite: func(next http.Handler) http.Handler {
			return admission.incomingGuard(r.rolloutOwner(admission.identityGuard(next, false), true))
		},
		Agent: func(next http.Handler) http.Handler {
			return admission.incomingGuard(r.rolloutAgent(admission.identityGuard(next, true)))
		},
	})
}

func newRolloutAdmission() *rolloutAdmission {
	return &rolloutAdmission{
		incoming: newRolloutBudget(rolloutBudgetPolicy{burst: 64, rate: 32, globalBurst: 256, globalRate: 128, concurrent: 64}),
		owners:   newRolloutBudget(rolloutBudgetPolicy{burst: 32, rate: 16, globalBurst: 128, globalRate: 64, concurrent: 16}),
		agents:   newRolloutBudget(rolloutBudgetPolicy{burst: 16, rate: 8, globalBurst: 128, globalRate: 64, concurrent: 32}),
	}
}

func (r *Router) rolloutOwner(next http.Handler, recent bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if r.security == nil {
			middleware.WriteAuthError(w, http.StatusServiceUnavailable, "administrator session is unavailable")
			return
		}
		guarded := r.ownerAdmin(next)
		if recent {
			guarded = r.recentAuthOwnerAdmin(next)
		}
		guarded.ServeHTTP(w, request)
	})
}

func (a *rolloutAdmission) incomingGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rolloutHeaders(w)
		ip, err := netip.ParseAddr(middleware.ClientIP(r))
		if err != nil {
			middleware.WriteAuthError(w, http.StatusBadRequest, "invalid client address")
			return
		}
		release, ok := a.incoming.acquire(ip.WithZone("").Unmap().String(), time.Now())
		if !ok {
			rolloutLimited(w)
			return
		}
		defer release()
		maximum := int64(agentrollouthttp.MaximumJSONBytes)
		if r.Pattern == rolloutUploadPattern {
			maximum = agentrollouthttp.MaximumUploadBytes
		}
		if r.ContentLength > maximum {
			middleware.WriteAuthError(w, http.StatusRequestEntityTooLarge, "rollout request body is too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maximum)
		if r.Pattern != rolloutUploadPattern {
			deadlineErr := http.NewResponseController(w).SetReadDeadline(time.Now().Add(rolloutReadTimeout))
			if deadlineErr != nil && !errors.Is(deadlineErr, http.ErrNotSupported) {
				middleware.WriteAuthError(w, http.StatusServiceUnavailable, "rollout read deadline is unavailable")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *rolloutAdmission) identityGuard(next http.Handler, agent bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, budget, ok := a.identity(r, agent)
		if !ok {
			middleware.WriteAuthError(w, http.StatusUnauthorized, "rollout identity is unavailable")
			return
		}
		release, allowed := budget.acquire(key, time.Now())
		if !allowed {
			rolloutLimited(w)
			return
		}
		defer release()
		defer func() {
			if recover() != nil {
				middleware.WriteAuthError(w, http.StatusInternalServerError, "rollout request failed")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *rolloutAdmission) identity(r *http.Request, agent bool) (string, *rolloutBudget, bool) {
	if agent {
		identity, ok := middleware.AgentIdentity(r.Context())
		return identity.NodeID, a.agents, ok && identity.Mode == model.AgentCredentialActive && identity.NodeID != ""
	}
	principal, ok := middleware.AdminPrincipal(r.Context())
	return principal.Session.ID, a.owners, ok && principal.Session.ID != ""
}

func rolloutHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func rolloutLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	middleware.WriteAuthError(w, http.StatusTooManyRequests, "rollout admission limit reached")
}

func (r *Router) rolloutAgent(next http.Handler) http.Handler {
	authenticated := middleware.AgentReportAuth(r.nodeStore, "", nil)(next)
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if r.nodeStore == nil {
			middleware.WriteAuthError(w, http.StatusServiceUnavailable, "agent authentication is unavailable")
			return
		}
		if len(request.Header.Values("Authorization")) != 1 || len(request.Header.Get("Authorization")) > rolloutMaximumAuthorizationBytes {
			middleware.WriteAuthError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		authenticated.ServeHTTP(w, request)
	})
}
