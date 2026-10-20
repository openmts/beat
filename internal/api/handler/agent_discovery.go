package handler

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/store"
)

// discoveryClockSkew 是签名时间戳允许的偏移窗口；超出即视为重放或漂移。
const discoveryClockSkew = 2 * time.Minute

// AgentDiscoveryOperations 是自动发现式入网协议需要的存储能力。
type AgentDiscoveryOperations interface {
	GrantBySecret(context.Context, string, time.Time) (*store.EnrollmentGrant, error)
	CreateChallenge(context.Context, store.EnrollmentChallengeInput) (store.EnrollmentChallenge, error)
	ConsumeChallenge(context.Context, store.EnrollmentChallengeProof) (store.EnrollmentChallenge, error)
	CreateOrResumeRequest(context.Context, store.EnrollmentRequestInput) (store.EnrollmentRequest, bool, error)
	EnrollmentRequest(context.Context, string) (*store.EnrollmentRequest, error)
	RotateEnrollmentTicket(context.Context, string, agentenrollment.Secret, time.Time) (store.EnrollmentRequest, error)
	ClaimEnrollmentRequest(context.Context, store.EnrollmentClaim, agentcredential.Token, time.Time) (store.EnrollmentClaimResult, error)
}

// discoveryBudgets 汇总四条协议路径各自的限流桶，避免任一入口拖垮其他入口。
type discoveryBudgets struct {
	challenge, request, poll, claim, rejected *enrollmentBudget
}

func newDiscoveryBudgets() discoveryBudgets {
	return discoveryBudgets{
		challenge: newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 10, total: 240, concurrent: 32, keys: 2048}),
		request:   newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 10, total: 240, concurrent: 32, keys: 2048}),
		poll:      newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 120, total: 2400, concurrent: 64, keys: 4096}),
		claim:     newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 30, total: 600, concurrent: 32, keys: 2048}),
		rejected:  newEnrollmentBudget(enrollmentBudgetPolicy{perIP: 60, total: 1200, concurrent: 64, keys: 4096}),
	}
}

// AgentDiscoveryHandler 实现 Agent 自动发现入网的公开协议端点。
type AgentDiscoveryHandler struct {
	store           AgentDiscoveryOperations
	origin          func(*http.Request) string
	budgets         discoveryBudgets
	now             func() time.Time
	newChallenge    func() ([]byte, error)
	newRequestToken func() (agentenrollment.Secret, error)
	newTicket       func() (agentenrollment.Secret, error)
	newNodeToken    func() (agentcredential.Token, error)
	secureOnly      bool
	metrics         EnrollmentMetrics
}

// NewAgentDiscoveryHandler 构造协议处理器；origin 给出签名绑定的规范来源。
func NewAgentDiscoveryHandler(operations AgentDiscoveryOperations, origin func(*http.Request) string,
	secureOnly bool) *AgentDiscoveryHandler {
	return &AgentDiscoveryHandler{
		store: operations, origin: origin, secureOnly: secureOnly, budgets: newDiscoveryBudgets(),
		now: time.Now, newChallenge: agentenrollment.GenerateChallenge,
		newRequestToken: agentenrollment.GenerateRequestToken, newTicket: agentenrollment.GenerateClaimTicket,
		newNodeToken: agentcredential.Generate,
	}
}

// HandleChallenge 发放一次性挑战：调用方必须持有可用的授权秘密。
func (h *AgentDiscoveryHandler) HandleChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	release, ok := h.begin(w, r, h.budgets.challenge)
	if !ok {
		return
	}
	defer release()
	grant, ok := h.authenticateGrant(w, r)
	if !ok {
		return
	}
	var body agentChallengeRequest
	if !parseDiscoveryJSON(w, r, &body) {
		return
	}
	publicKey, ok := decodeDiscoveryPublicKey(body.PublicKey)
	if !ok {
		discoveryReject(w, h, r)
		return
	}
	digest, ok := decodeDiscoveryDigest(body.MetadataDigest)
	if !ok {
		discoveryReject(w, h, r)
		return
	}
	address, ok := discoveryClientAddress(r)
	if !ok || !grantAllowsSource(grant, address) {
		discoveryReject(w, h, r)
		return
	}
	challenge, err := h.newChallenge()
	if err != nil {
		JSONError(w, http.StatusServiceUnavailable, "enrollment is temporarily unavailable")
		return
	}
	now := h.now().UTC()
	expiresAt := now.Add(agentDiscoveryChallengeTTL)
	hash := agentenrollment.Digest(challenge)
	fingerprint := agentenrollment.Fingerprint(publicKey)
	created, err := h.store.CreateChallenge(r.Context(), store.EnrollmentChallengeInput{
		GrantID: grant.ID, ChallengeHash: hash[:], KeyFingerprint: fingerprint[:], MetadataDigest: digest,
		SourceIP: address.String(), Now: now, ExpiresAt: expiresAt,
	})
	if err != nil {
		h.respondStoreError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, agentChallengeResponse{
		ChallengeID: created.ID, Challenge: encodeDiscoveryBytes(challenge), Origin: h.canonicalOrigin(r),
		IssuedAt: formatDiscoveryInstant(now), ExpiresAt: formatDiscoveryInstant(expiresAt),
		ProtocolVersion: agentDiscoveryProtocolVersion,
	})
}

// enrollmentProtocolAuditContext 为公开协议路径提供匿名审计来源：只有请求 ID 与来源地址，
// 不写入任何机器标识、授权秘密或请求令牌。
func enrollmentProtocolAuditContext(request *http.Request) context.Context {
	return store.WithEnrollmentAuditContext(request.Context(), store.EnrollmentAuditContext{
		RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request),
		UserAgent: request.UserAgent(),
	})
}

// begin 校验传输安全与限流，并返回必须在处理结束后调用的释放函数。
func (h *AgentDiscoveryHandler) begin(w http.ResponseWriter, r *http.Request,
	budget *enrollmentBudget) (func(), bool) {
	if h.secureOnly && !middleware.IsSecure(r) {
		JSONError(w, http.StatusBadRequest, "enrollment requires https")
		return nil, false
	}
	address, ok := discoveryClientAddress(r)
	if !ok {
		JSONError(w, http.StatusBadRequest, "invalid client source address")
		return nil, false
	}
	release, ok := budget.acquire(address.String(), h.now())
	if !ok {
		enrollmentLimited(w)
		return nil, false
	}
	return release, true
}

// authenticateGrant 由 bearer 头解析授权秘密；未知、撤销、过期与容量耗尽对外一致。
func (h *AgentDiscoveryHandler) authenticateGrant(w http.ResponseWriter, r *http.Request) (*store.EnrollmentGrant, bool) {
	plaintext, ok := enrollmentGrantSecret(r)
	if !ok {
		discoveryReject(w, h, r)
		return nil, false
	}
	if _, valid := decodeDiscoverySecret(plaintext, agentenrollment.GrantSecretPrefix); !valid {
		discoveryReject(w, h, r)
		return nil, false
	}
	grant, err := h.store.GrantBySecret(r.Context(), plaintext, h.now().UTC())
	if err != nil {
		discoveryReject(w, h, r)
		return nil, false
	}
	return grant, true
}

// canonicalOrigin 返回签名绑定的规范来源，未配置外部地址时按请求推导。
func (h *AgentDiscoveryHandler) canonicalOrigin(r *http.Request) string {
	if h.origin == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(h.origin(r)), "/")
}

// respondStoreError 把存储层错误映射为固定的公开响应。
func (h *AgentDiscoveryHandler) respondStoreError(w http.ResponseWriter, err error) {
	switch {
	case errorsIsStoreBusy(err):
		enrollmentLimited(w)
	case isDiscoveryClientError(err):
		JSONError(w, http.StatusBadRequest, "enrollment request was rejected")
	default:
		JSONError(w, http.StatusServiceUnavailable, "enrollment is temporarily unavailable")
	}
}

// discoveryClientAddress 返回规范化后的来源地址；带 zone 的地址一律拒绝。
func discoveryClientAddress(r *http.Request) (netip.Addr, bool) {
	address, err := netip.ParseAddr(middleware.ClientIP(r))
	if err != nil || address.Zone() != "" {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

// grantAllowsSource 判断来源地址是否落在授权的地址范围内。
func grantAllowsSource(grant *store.EnrollmentGrant, address netip.Addr) bool {
	policy, err := agentenrollment.CompileSourcePolicy(grant.SourceCIDRs, grant.HostnamePattern)
	if err != nil {
		return false
	}
	return policy.AllowsSource(address)
}
