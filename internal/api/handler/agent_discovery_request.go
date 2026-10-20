package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// discoveryRequestCreate 是校验通过的建请求材料，避免在主流程里重复解码。
type discoveryRequestCreate struct {
	publicKey   []byte
	challenge   []byte
	digest      []byte
	metadata    agentenrollment.Metadata
	canonical   []byte
	fingerprint [sha256.Size]byte
	signature   []byte
	challengeID string
}

// HandleCreateRequest 创建或恢复一条入网请求：消费挑战、验签并交付一次性请求令牌。
func (h *AgentDiscoveryHandler) HandleCreateRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, ok := h.begin(w, r, h.budgets.request)
	if !ok {
		return
	}
	defer release()
	grant, ok := h.authenticateGrant(w, r)
	if !ok {
		return
	}
	var body agentRequestCreatePayload
	if !parseDiscoveryJSON(w, r, &body) {
		return
	}
	input, ok := decodeRequestCreate(body)
	if !ok {
		discoveryReject(w, h, r)
		return
	}
	address, ok := discoveryClientAddress(r)
	if !ok || !grantAllowsSource(grant, address) || !grantAllowsHostname(grant, input.metadata.Hostname) {
		discoveryReject(w, h, r)
		return
	}
	challenge, err := h.store.ConsumeChallenge(r.Context(), store.EnrollmentChallengeProof{
		ID: input.challengeID, Challenge: input.challenge, KeyFingerprint: input.fingerprint[:],
		MetadataDigest: input.digest, SourceIP: address.String(), Now: h.now().UTC(),
	})
	if err != nil {
		h.respondStoreError(w, err)
		return
	}
	if challenge.GrantID != grant.ID || !h.verifyRequestSignature(r, challenge, input) {
		discoveryReject(w, h, r)
		return
	}
	response, err := h.persistRequest(r, grant, input, address.String())
	if err != nil {
		h.respondStoreError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, response)
}

// persistRequest 写入或恢复请求，并把一次性令牌放进响应。
func (h *AgentDiscoveryHandler) persistRequest(r *http.Request, grant *store.EnrollmentGrant,
	input discoveryRequestCreate, sourceIP string) (agentRequestCreateResponse, error) {
	token, err := h.newRequestToken()
	if err != nil {
		return agentRequestCreateResponse{}, err
	}
	request, resumed, err := h.store.CreateOrResumeRequest(r.Context(), store.EnrollmentRequestInput{
		GrantID: grant.ID, Fingerprint: input.fingerprint[:], PublicKey: input.publicKey,
		Token: token, Metadata: input.metadata, SourceIP: sourceIP, Now: h.now().UTC(),
	})
	if err != nil {
		return agentRequestCreateResponse{}, err
	}
	if resumed && h.metrics != nil {
		h.metrics.ObserveEnrollmentRequestResume()
	}
	h.observeAutomaticApprovalLatency(request)
	return agentRequestCreateResponse{RequestID: request.ID, RequestToken: token.Plaintext,
		Status: string(request.Status), GrantName: grant.Name}, nil
}

// verifyRequestSignature 校验签名覆盖挑战、来源、密钥指纹与元数据。
func (h *AgentDiscoveryHandler) verifyRequestSignature(r *http.Request, challenge store.EnrollmentChallenge,
	input discoveryRequestCreate) bool {
	binding := agentenrollment.RequestBinding{
		ChallengeID: challenge.ID, Challenge: input.challenge, Origin: h.canonicalOrigin(r),
		Fingerprint: input.fingerprint[:], Metadata: input.canonical, ExpiresAt: challenge.ExpiresAt,
	}
	return agentenrollment.VerifySignature(input.publicKey, binding.Canonical(), input.signature) == nil
}

// decodeRequestCreate 解码并交叉校验建请求材料，任一不符直接拒绝。
func decodeRequestCreate(body agentRequestCreatePayload) (discoveryRequestCreate, bool) {
	publicKey, ok := decodeDiscoveryPublicKey(body.PublicKey)
	if !ok || strings.TrimSpace(body.ChallengeID) == "" {
		return discoveryRequestCreate{}, false
	}
	challenge, ok := decodeDiscoveryFixed(body.Challenge, agentenrollment.ChallengeSize)
	if !ok {
		return discoveryRequestCreate{}, false
	}
	digest, ok := decodeDiscoveryDigest(body.MetadataDigest)
	if !ok {
		return discoveryRequestCreate{}, false
	}
	signature, ok := decodeDiscoverySignature(body.Signature)
	if !ok {
		return discoveryRequestCreate{}, false
	}
	metadata := metadataFromPayload(body.Metadata)
	if err := metadata.Validate(); err != nil {
		return discoveryRequestCreate{}, false
	}
	canonical := metadata.Canonical()
	computed := sha256.Sum256(canonical)
	if subtle.ConstantTimeCompare(computed[:], digest) != 1 {
		return discoveryRequestCreate{}, false
	}
	return discoveryRequestCreate{publicKey: publicKey, challenge: challenge, digest: digest, metadata: metadata,
		canonical: canonical, fingerprint: agentenrollment.Fingerprint(publicKey),
		signature: signature, challengeID: strings.TrimSpace(body.ChallengeID)}, true
}

// metadataFromPayload 把请求体元数据转换为领域结构；未上报 SSH 端口时按默认 22 处理。
func metadataFromPayload(payload agentMetadataPayload) agentenrollment.Metadata {
	port := payload.SSHPort
	if port == 0 {
		port = 22
	}
	return agentenrollment.Metadata{
		Hostname: strings.TrimSpace(payload.Hostname), AdvertisedHost: strings.TrimSpace(payload.AdvertisedHost),
		OS: strings.TrimSpace(payload.OS), Architecture: strings.TrimSpace(payload.Architecture),
		AgentVersion:   strings.TrimSpace(payload.AgentVersion),
		Virtualization: strings.TrimSpace(payload.Virtualization), SSHPort: port,
		InstallationNonce: strings.TrimSpace(payload.InstallationNonce),
	}
}

// HandleRequestStatus 轮询请求状态；审批通过后同时轮换一张两分钟票据。
func (h *AgentDiscoveryHandler) HandleRequestStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, ok := h.begin(w, r, h.budgets.poll)
	if !ok {
		return
	}
	defer release()
	request, ok := h.authenticateRequestToken(w, r)
	if !ok {
		return
	}
	var body agentStatusPayload
	if !parseDiscoveryJSON(w, r, &body) {
		return
	}
	if !h.verifyStatusSignature(r, request, body) {
		discoveryReject(w, h, r)
		return
	}
	response := agentStatusView(*request)
	if ticket, rotated, ok := h.rotateTicket(r.Context(), *request); ok {
		response.Ticket = ticket.Plaintext
		response.TicketGeneration = rotated.TicketGeneration
		if rotated.TicketExpiresAt.Valid {
			response.TicketExpiresAt = formatDiscoveryInstant(rotated.TicketExpiresAt.Time)
		}
	}
	JSONResponse(w, http.StatusOK, response)
}

// rotateTicket 只在审批通过或仍在恢复窗口内时轮换票据，其他状态保持无票据。
func (h *AgentDiscoveryHandler) rotateTicket(ctx context.Context,
	request store.EnrollmentRequest) (agentenrollment.Secret, store.EnrollmentRequest, bool) {
	if !h.ticketAvailable(request) {
		return agentenrollment.Secret{}, store.EnrollmentRequest{}, false
	}
	ticket, err := h.newTicket()
	if err != nil {
		return agentenrollment.Secret{}, store.EnrollmentRequest{}, false
	}
	rotated, err := h.store.RotateEnrollmentTicket(ctx, request.ID, ticket, h.now().UTC())
	if err != nil {
		return agentenrollment.Secret{}, store.EnrollmentRequest{}, false
	}
	return ticket, rotated, true
}

// ticketAvailable 判断当前状态是否允许交付新票据：审批通过，或认领后仍在受限恢复窗口内。
func (h *AgentDiscoveryHandler) ticketAvailable(request store.EnrollmentRequest) bool {
	if request.Status == agentenrollment.RequestApproved {
		return true
	}
	if request.Status != agentenrollment.RequestClaimed || !request.RecoveryDeadline.Valid {
		return false
	}
	if request.RecoveryAttempts >= agentenrollment.MaxClaimRecoveryAttempts {
		return false
	}
	return h.now().UTC().Before(request.RecoveryDeadline.Time)
}

// verifyStatusSignature 校验轮询签名的来源、时间戳与随机数。
func (h *AgentDiscoveryHandler) verifyStatusSignature(r *http.Request, request *store.EnrollmentRequest,
	body agentStatusPayload) bool {
	timestamp, ok := parseDiscoveryInstant(body.Timestamp)
	if !ok || !withinDiscoverySkew(timestamp, h.now()) {
		return false
	}
	nonce, ok := decodeDiscoveryNonce(body.Nonce)
	if !ok {
		return false
	}
	signature, ok := decodeDiscoverySignature(body.Signature)
	if !ok {
		return false
	}
	binding := agentenrollment.StatusBinding{RequestID: request.ID, Origin: h.canonicalOrigin(r),
		Timestamp: timestamp, Nonce: nonce}
	return agentenrollment.VerifySignature(request.PublicKey, binding.Canonical(), signature) == nil
}

// authenticateRequestToken 由 bearer 头解析请求令牌并与落库哈希常量时间比对。
func (h *AgentDiscoveryHandler) authenticateRequestToken(w http.ResponseWriter, r *http.Request) (*store.EnrollmentRequest, bool) {
	plaintext, ok := enrollmentGrantSecret(r)
	if !ok {
		discoveryReject(w, h, r)
		return nil, false
	}
	if _, valid := decodeDiscoverySecret(plaintext, agentenrollment.RequestTokenPrefix); !valid {
		discoveryReject(w, h, r)
		return nil, false
	}
	id := strings.TrimSpace(r.PathValue("id"))
	request, err := h.store.EnrollmentRequest(r.Context(), id)
	if err != nil || request == nil || !agentenrollment.SecretMatches(request.TokenHash, plaintext) {
		discoveryReject(w, h, r)
		return nil, false
	}
	return request, true
}

// agentStatusView 生成状态响应骨架，只暴露该请求自身的信息。
func agentStatusView(request store.EnrollmentRequest) agentStatusResponse {
	response := agentStatusResponse{RequestID: request.ID, Status: string(request.Status)}
	if request.Status == agentenrollment.RequestRejected {
		response.Reason = request.RejectionReason
	}
	if request.ReservedNodeID.Valid {
		response.NodeID = request.ReservedNodeID.String
		response.NodeName = request.ReservedName.String
	}
	return response
}

// grantAllowsHostname 判断上报主机名是否匹配授权的锚定正则。
func grantAllowsHostname(grant *store.EnrollmentGrant, hostname string) bool {
	policy, err := agentenrollment.CompileSourcePolicy(grant.SourceCIDRs, grant.HostnamePattern)
	if err != nil {
		return false
	}
	return policy.AllowsHostname(hostname)
}
