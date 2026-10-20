package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/store"
)

// AgentEnrollmentAdminOperations 是管理端入网视图依赖的存储能力。
type AgentEnrollmentAdminOperations interface {
	CreateGrant(context.Context, store.EnrollmentGrantInput, time.Time) (store.EnrollmentGrant, error)
	ListGrants(context.Context) ([]store.EnrollmentGrant, error)
	Grant(context.Context, string) (*store.EnrollmentGrant, error)
	UpdateGrant(context.Context, string, store.EnrollmentGrantInput, time.Time) (store.EnrollmentGrant, error)
	RotateGrantSecret(context.Context, string, agentenrollment.Secret, time.Time) (store.EnrollmentGrant, error)
	RevokeGrant(context.Context, string, string, time.Time) (store.EnrollmentGrant, error)
	ListEnrollmentRequests(context.Context, store.EnrollmentRequestFilter) (store.EnrollmentRequestPage, error)
	EnrollmentRequest(context.Context, string) (*store.EnrollmentRequest, error)
	ApproveEnrollmentRequest(context.Context, store.EnrollmentApproval, time.Time) (store.EnrollmentRequest, error)
	RejectEnrollmentRequest(context.Context, store.EnrollmentRejection, time.Time) (store.EnrollmentRequest, error)
}

// AgentEnrollmentAdminHandler 提供授权与请求的管理端读写接口。
type AgentEnrollmentAdminHandler struct {
	store          AgentEnrollmentAdminOperations
	now            func() time.Time
	generateSecret func() (agentenrollment.Secret, error)
	metrics        EnrollmentMetrics
}

// NewAgentEnrollmentAdminHandler 构造管理端处理器。
func NewAgentEnrollmentAdminHandler(operations AgentEnrollmentAdminOperations) *AgentEnrollmentAdminHandler {
	return &AgentEnrollmentAdminHandler{store: operations, now: time.Now, generateSecret: agentenrollment.GenerateGrantSecret}
}

// enrollmentAdminAuditContext 把已认证主体与请求来源写进上下文，供存储层原子写入审计行。
// 审计行不含授权秘密、请求令牌或任何秘密前缀。
func enrollmentAdminAuditContext(request *http.Request) context.Context {
	audit := store.EnrollmentAuditContext{RequestID: middleware.RequestID(request.Context()),
		IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent()}
	if principal, ok := middleware.AdminPrincipal(request.Context()); ok {
		audit.ActorID, audit.ActorUsername = principal.User.ID, principal.User.Username
	}
	return store.WithEnrollmentAuditContext(request.Context(), audit)
}

// actorID 取出当前管理会话的操作者 ID；调用方已在路由层校验角色。
func actorID(request *http.Request) string {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		return ""
	}
	return principal.User.ID
}

func (handler *AgentEnrollmentAdminHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrEnrollmentGrantNotFound), errors.Is(err, store.ErrEnrollmentRequestNotFound):
		JSONError(w, http.StatusNotFound, "enrollment record was not found")
	case errors.Is(err, store.ErrEnrollmentGrantNameTaken):
		JSONError(w, http.StatusConflict, "enrollment grant name is already in use")
	case errors.Is(err, store.ErrEnrollmentGrantCapacity):
		JSONError(w, http.StatusConflict, "enrollment grant capacity is exhausted")
	case errors.Is(err, store.ErrNodeNameConflict):
		JSONError(w, http.StatusConflict, "a node with that name already exists")
	case errors.Is(err, store.ErrEnrollmentRequestUnavailable):
		JSONError(w, http.StatusConflict, "enrollment request is no longer decidable")
	case errors.Is(err, agentenrollment.ErrInvalidGrantPolicy), errors.Is(err, agentenrollment.ErrInvalidMetadata):
		JSONError(w, http.StatusBadRequest, err.Error())
	default:
		JSONError(w, http.StatusInternalServerError, "enrollment operation failed")
	}
}

// decodeAdminBody 解码管理端载荷，统一 32 KiB 预算并拒绝未知字段。
func decodeAdminBody(w http.ResponseWriter, request *http.Request, target any) bool {
	if err := DecodeJSONBody(request, target, agentDiscoveryMaxBodyBytes, true); err != nil {
		WriteBodyError(w, err)
		return false
	}
	return true
}

func enrollmentGrantView(grant store.EnrollmentGrant) enrollmentGrantPayload {
	view := enrollmentGrantPayload{
		ID: grant.ID, Name: grant.Name, SecretPrefix: grant.SecretPrefix, Mode: string(grant.Mode),
		Enabled: grant.Enabled, ExpiresAt: formatEnrollmentInstant(grant.ExpiresAt), DefaultGroupID: grant.DefaultGroupID,
		DefaultGroupName: grant.DefaultGroupName, NodeNamePrefix: grant.NodeNamePrefix, Tags: nonNilStrings(grant.Tags),
		Remarks: grant.Remarks, Public: grant.Public, SSHPort: grant.SSHPort, SourceIPAsHost: grant.SourceIPAsHost,
		SourceCIDRs: nonNilStrings(grant.SourceCIDRs), HostnamePattern: grant.HostnamePattern,
		ClaimRecovery: grant.ClaimRecovery, MaximumEnrollments: grant.MaximumEnrollments,
		ConsumedEnrollments: grant.ConsumedEnrollments, CreatedAt: formatEnrollmentInstant(grant.CreatedAt),
		UpdatedAt: formatEnrollmentInstant(grant.UpdatedAt),
	}
	if grant.RevokedAt.Valid {
		view.RevokedAt = formatEnrollmentInstant(grant.RevokedAt.Time)
	}
	return view
}

func enrollmentRequestView(request store.EnrollmentRequest) enrollmentRequestPayload {
	name := request.NodeNameSnapshot
	if request.ReservedName.Valid && request.ReservedName.String != "" {
		name = request.ReservedName.String
	}
	view := enrollmentRequestPayload{
		ID: request.ID, GrantID: request.GrantID, GrantName: request.GrantName, Status: string(request.Status),
		NodeName: name, Hostname: request.Metadata.Hostname, AdvertisedHost: request.Metadata.AdvertisedHost,
		OS: request.Metadata.OS, Architecture: request.Metadata.Architecture,
		AgentVersion: request.Metadata.AgentVersion, Virtualization: request.Metadata.Virtualization,
		SSHPort: request.Metadata.SSHPort, SourceIP: request.SourceIP,
		InstallationNonce: request.Metadata.InstallationNonce,
		FirstRequestAt:    formatEnrollmentInstant(request.FirstRequestAt), LastRequestAt: formatEnrollmentInstant(request.LastRequest),
		ExpiresAt: formatEnrollmentInstant(request.RequestExpiresAt), Reason: request.RejectionReason,
		ApprovedBy: request.ApprovedBy, RejectedBy: request.RejectedBy,
	}
	if request.ReservedNodeID.Valid {
		view.ReservedNodeID = request.ReservedNodeID.String
	}
	if request.ClaimedAt.Valid {
		view.ClaimedAt = formatEnrollmentInstant(request.ClaimedAt.Time)
	}
	return view
}

func formatEnrollmentInstant(instant time.Time) string {
	if instant.IsZero() {
		return ""
	}
	return instant.UTC().Format(time.RFC3339)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
