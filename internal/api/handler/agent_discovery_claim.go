package handler

import (
	"net/http"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/store"
)

// HandleClaim 用一次性票据兑换节点凭据；节点令牌只在本次响应里出现。
func (h *AgentDiscoveryHandler) HandleClaim(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, ok := h.begin(w, r, h.budgets.claim)
	if !ok {
		return
	}
	defer release()
	request, ok := h.authenticateRequestToken(w, r)
	if !ok {
		return
	}
	var body agentClaimPayload
	if !parseDiscoveryJSON(w, r, &body) {
		return
	}
	ticket, ok := h.decodeClaimTicket(r, request, body)
	if !ok {
		discoveryReject(w, h, r)
		return
	}
	token, err := h.newNodeToken()
	if err != nil {
		JSONError(w, http.StatusServiceUnavailable, "enrollment is temporarily unavailable")
		return
	}
	result, err := h.store.ClaimEnrollmentRequest(enrollmentProtocolAuditContext(r),
		store.EnrollmentClaim{RequestID: request.ID, Ticket: ticket.Plaintext}, token, h.now().UTC())
	if err != nil {
		h.respondStoreError(w, err)
		return
	}
	if request.ApprovedAt.Valid {
		h.observeClaimLatency(request.ApprovedAt.Time)
	}
	JSONResponse(w, http.StatusCreated, agentClaimResponse{
		NodeID: result.NodeID, NodeName: result.NodeName, GroupID: result.GroupID,
		ServerURL: h.canonicalOrigin(r), Host: result.Host, Port: result.Port,
		AgentToken: token.Plaintext, ReportInterval: agentjoin.DefaultReportInterval,
		Recovered: result.Recovered,
	})
}

// decodeClaimTicket 校验票据格式并验证签名覆盖请求、保留节点与规范来源。
func (h *AgentDiscoveryHandler) decodeClaimTicket(r *http.Request, request *store.EnrollmentRequest,
	body agentClaimPayload) (agentenrollment.Secret, bool) {
	ticket, ok := decodeDiscoverySecret(body.Ticket, agentenrollment.ClaimTicketPrefix)
	if !ok {
		return agentenrollment.Secret{}, false
	}
	timestamp, ok := parseDiscoveryInstant(body.Timestamp)
	if !ok || !withinDiscoverySkew(timestamp, h.now()) {
		return agentenrollment.Secret{}, false
	}
	nonce, ok := decodeDiscoveryNonce(body.Nonce)
	if !ok {
		return agentenrollment.Secret{}, false
	}
	signature, ok := decodeDiscoverySignature(body.Signature)
	if !ok {
		return agentenrollment.Secret{}, false
	}
	nodeID := ""
	if request.ReservedNodeID.Valid {
		nodeID = request.ReservedNodeID.String
	}
	binding := agentenrollment.ClaimBinding{RequestID: request.ID, NodeID: nodeID, Ticket: ticket.Plaintext,
		Origin: h.canonicalOrigin(r), Timestamp: timestamp, Nonce: nonce}
	if agentenrollment.VerifySignature(request.PublicKey, binding.Canonical(), signature) != nil {
		return agentenrollment.Secret{}, false
	}
	return ticket, true
}
