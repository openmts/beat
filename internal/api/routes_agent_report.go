package api

import (
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/ingress"
	"github.com/beat/backend/internal/model"
)

func (r *Router) agentReport(next http.Handler) http.Handler {
	if !r.agentAuthEnabled {
		return next
	}
	resolver := bodyNodeName("name")
	authenticated := middleware.AgentReportAuth(r.nodeStore, r.legacyAgentToken, resolver)(r.agentIngestAdmission(next))
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(w, request.Body, model.MaximumAgentReportBytes)
		authenticated.ServeHTTP(w, request)
	})
}

// agentIngestAdmission 在认证后、任何心跳或 MTS 写入前，按认证节点 ID 再取一次号。
// 外层中间件保留粗粒度的来源封顶，这里保证每个节点只有一个在途上报。
func (r *Router) agentIngestAdmission(next http.Handler) http.Handler {
	if r.ingressLimiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		identity, ok := middleware.AgentIdentity(request.Context())
		if !ok {
			next.ServeHTTP(w, request)
			return
		}
		lease, rejection := r.ingressLimiter.Admit(ingress.AgentWrite, "node:"+middleware.Fingerprint(identity.NodeID))
		if rejection != nil {
			middleware.WriteAdmissionRejection(w, rejection)
			return
		}
		defer lease.Release()
		next.ServeHTTP(w, request)
	})
}
