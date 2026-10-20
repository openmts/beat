package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// HandleListGrants 返回全部入网授权，隐藏秘密明文只保留展示前缀。
func (handler *AgentEnrollmentAdminHandler) HandleListGrants(w http.ResponseWriter, request *http.Request) {
	grants, err := handler.store.ListGrants(request.Context())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	views := make([]enrollmentGrantPayload, 0, len(grants))
	for _, grant := range grants {
		views = append(views, enrollmentGrantView(grant))
	}
	JSONResponse(w, http.StatusOK, enrollmentGrantListResponse{Grants: views})
}

// HandleCreateGrant 创建授权并一次性返回秘密明文。
func (handler *AgentEnrollmentAdminHandler) HandleCreateGrant(w http.ResponseWriter, request *http.Request) {
	var payload enrollmentGrantUpsertPayload
	if !decodeAdminBody(w, request, &payload) {
		return
	}
	policy, err := parseGrantPolicy(payload)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	secret, err := handler.generateSecret()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to generate enrollment secret")
		return
	}
	grant, err := handler.store.CreateGrant(enrollmentAdminAuditContext(request), store.EnrollmentGrantInput{
		Policy: policy, Secret: secret, ActorID: actorID(request)}, handler.now().UTC())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, enrollmentGrantSecretResponse{
		Grant: enrollmentGrantView(grant), Secret: secret.Plaintext})
}

// HandleGetGrant 读取单条授权。
func (handler *AgentEnrollmentAdminHandler) HandleGetGrant(w http.ResponseWriter, request *http.Request) {
	grant, err := handler.store.Grant(request.Context(), request.PathValue("id"))
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		Grant enrollmentGrantPayload `json:"grant"`
	}{Grant: enrollmentGrantView(*grant)})
}

// HandleUpdateGrant 覆盖授权策略，秘密与已消耗名额保持不变。
func (handler *AgentEnrollmentAdminHandler) HandleUpdateGrant(w http.ResponseWriter, request *http.Request) {
	var payload enrollmentGrantUpsertPayload
	if !decodeAdminBody(w, request, &payload) {
		return
	}
	policy, err := parseGrantPolicy(payload)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	grant, err := handler.store.UpdateGrant(enrollmentAdminAuditContext(request), request.PathValue("id"),
		store.EnrollmentGrantInput{Policy: policy, ActorID: actorID(request)}, handler.now().UTC())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		Grant enrollmentGrantPayload `json:"grant"`
	}{Grant: enrollmentGrantView(grant)})
}

// HandleRotateGrantSecret 轮换授权秘密并作废未使用的挑战。
func (handler *AgentEnrollmentAdminHandler) HandleRotateGrantSecret(w http.ResponseWriter, request *http.Request) {
	secret, err := handler.generateSecret()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to generate enrollment secret")
		return
	}
	grant, err := handler.store.RotateGrantSecret(enrollmentAdminAuditContext(request), request.PathValue("id"),
		secret, handler.now().UTC())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, enrollmentGrantSecretResponse{
		Grant: enrollmentGrantView(grant), Secret: secret.Plaintext})
}

// HandleRevokeGrant 撤销授权：拒绝未认领请求并清理保留节点。
func (handler *AgentEnrollmentAdminHandler) HandleRevokeGrant(w http.ResponseWriter, request *http.Request) {
	grant, err := handler.store.RevokeGrant(enrollmentAdminAuditContext(request), request.PathValue("id"),
		actorID(request), handler.now().UTC())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		Grant enrollmentGrantPayload `json:"grant"`
	}{Grant: enrollmentGrantView(grant)})
}

// parseGrantPolicy 把管理端载荷转换为授权策略，时间字段必须是 RFC3339。
func parseGrantPolicy(payload enrollmentGrantUpsertPayload) (agentenrollment.GrantPolicy, error) {
	expires, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.ExpiresAt))
	if err != nil {
		return agentenrollment.GrantPolicy{}, fmt.Errorf("%w: expires_at must be RFC3339",
			agentenrollment.ErrInvalidGrantPolicy)
	}
	return agentenrollment.GrantPolicy{
		Name: payload.Name, Mode: agentenrollment.ApprovalMode(payload.Mode),
		MaximumEnrollments: payload.MaximumEnrollments, DefaultGroupID: payload.DefaultGroupID,
		NodeNamePrefix: payload.NodeNamePrefix, Tags: payload.Tags, Remarks: payload.Remarks,
		Public: payload.Public, SSHPort: payload.SSHPort, UseSourceIPAsHost: payload.UseSourceIPAsHost,
		SourceCIDRs: payload.SourceCIDRs, HostnamePattern: payload.HostnamePattern,
		ClaimRecovery: payload.ClaimRecovery, Enabled: payload.Enabled, ExpiresAt: expires.UTC(),
	}, nil
}
