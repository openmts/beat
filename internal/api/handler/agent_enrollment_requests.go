package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/store"
)

// HandleListRequests 返回一页入网请求，支持的过滤器全部来自白名单。
func (handler *AgentEnrollmentAdminHandler) HandleListRequests(w http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := store.EnrollmentRequestFilter{
		Status:  agentenrollment.RequestStatus(strings.TrimSpace(query.Get("status"))),
		GrantID: strings.TrimSpace(query.Get("grant_id")),
		Cursor:  strings.TrimSpace(query.Get("cursor")),
	}
	if filter.Status != "" && !filter.Status.Valid() {
		JSONError(w, http.StatusBadRequest, "unknown enrollment request status")
		return
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			JSONError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		filter.Limit = limit
	}
	page, err := handler.store.ListEnrollmentRequests(request.Context(), filter)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	views := make([]enrollmentRequestPayload, 0, len(page.Requests))
	for _, item := range page.Requests {
		views = append(views, enrollmentRequestView(item))
	}
	JSONResponse(w, http.StatusOK, enrollmentRequestListResponse{Requests: views, NextCursor: page.NextCursor})
}

// HandleGetRequest 读取单条请求。
func (handler *AgentEnrollmentAdminHandler) HandleGetRequest(w http.ResponseWriter, request *http.Request) {
	record, err := handler.store.EnrollmentRequest(request.Context(), request.PathValue("id"))
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		Request enrollmentRequestPayload `json:"request"`
	}{Request: enrollmentRequestView(*record)})
}

// HandleApproveRequest 审批请求并保留隐藏节点。
func (handler *AgentEnrollmentAdminHandler) HandleApproveRequest(w http.ResponseWriter, request *http.Request) {
	var payload enrollmentApprovalPayload
	if !decodeAdminBody(w, request, &payload) {
		return
	}
	updated, err := handler.store.ApproveEnrollmentRequest(enrollmentAdminAuditContext(request), store.EnrollmentApproval{
		RequestID: request.PathValue("id"), NodeName: payload.NodeName, GroupID: payload.GroupID,
		Tags: payload.Tags, Remarks: payload.Remarks, Public: payload.Public, Host: payload.Host,
		SSHPort: payload.SSHPort, ActorID: actorID(request)}, handler.now().UTC())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	handler.observeApprovalLatency(updated)
	JSONResponse(w, http.StatusOK, struct {
		Request enrollmentRequestPayload `json:"request"`
	}{Request: enrollmentRequestView(updated)})
}

// HandleRejectRequest 拒绝请求并清理隐藏节点保留。
func (handler *AgentEnrollmentAdminHandler) HandleRejectRequest(w http.ResponseWriter, request *http.Request) {
	var payload enrollmentRejectionPayload
	if !decodeAdminBody(w, request, &payload) {
		return
	}
	updated, err := handler.store.RejectEnrollmentRequest(enrollmentAdminAuditContext(request), store.EnrollmentRejection{
		RequestID: request.PathValue("id"), ActorID: actorID(request),
		Reason: strings.TrimSpace(payload.Reason)}, handler.now().UTC())
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		Request enrollmentRequestPayload `json:"request"`
	}{Request: enrollmentRequestView(updated)})
}
