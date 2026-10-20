package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (handler *AdminAuthHandler) HandleTOTP(w http.ResponseWriter, request *http.Request) {
	request = withAuthenticationAudit(request)
	w.Header().Set("Cache-Control", "no-store")
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	if body.Code == "" {
		setup, err := handler.service.BeginTOTP(request.Context(), &principal)
		if err != nil {
			writeSecurityError(w, err)
			return
		}
		JSONResponse(w, http.StatusOK, setup)
		return
	}
	if err := handler.service.EnableTOTP(request.Context(), &principal, body.Code); err != nil {
		writeIdentityProofError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleDisableTOTP(w http.ResponseWriter, request *http.Request) {
	request = withAuthenticationAudit(request)
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	if err := handler.service.DisableTOTP(request.Context(), &principal); err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleListSessions(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	limit, err := sessionListLimit(request)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid session query")

		return
	}
	sessions, err := handler.service.ListSessions(request.Context(), &principal, limit)
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, sessions)
}

// sessionListLimit 只接受 1..200 的显式分页上限，缺省为固定页大小。
func sessionListLimit(request *http.Request) (int, error) {
	raw := request.URL.Query().Get("limit")
	if raw == "" {
		return model.AdminSessionPageSize, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > model.AdminSessionMaximumPage {
		return 0, store.ErrInvalidAuditEventQuery
	}
	return limit, nil
}

func (handler *AdminAuthHandler) HandleRevokeSession(w http.ResponseWriter, request *http.Request) {
	request = withAuthenticationAudit(request)
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	if err := handler.service.RevokeSession(request.Context(), &principal, request.PathValue("id")); err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleRevokeOtherSessions(w http.ResponseWriter, request *http.Request) {
	request = withAuthenticationAudit(request)
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	count, err := handler.service.RevokeOtherSessions(request.Context(), &principal)
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		Revoked int64 `json:"revoked"`
	}{Revoked: count})
}

func (handler *AdminAuthHandler) HandleAuditEvents(w http.ResponseWriter, request *http.Request) {
	filter, err := auditEventFilter(request)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid audit event query")

		return
	}
	if _, ok := middleware.ServicePrincipalFrom(request.Context()); ok {
		page, err := handler.service.ListAuditEventsForService(request.Context(), filter)
		if err != nil {
			writeAuditEventError(w, err)
			return
		}
		JSONResponse(w, http.StatusOK, page)
		return
	}
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	page, err := handler.service.ListAuditEvents(request.Context(), &principal, filter)
	if err != nil {
		writeAuditEventError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, page)
}

// auditEventFilter 解析游标分页输入；非数字或越界的 limit 一律拒绝，绝不在服务端静默夹紧。
func auditEventFilter(request *http.Request) (model.AuditFilter, error) {
	values := request.URL.Query()
	filter := model.AuditFilter{Action: values.Get("action"), ActorID: values.Get("actor_id"),
		Cursor: values.Get("cursor")}
	raw := values.Get("limit")
	if raw == "" {
		return filter, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > store.AuditMaximumPage {
		return filter, store.ErrInvalidAuditEventQuery
	}
	filter.Limit = limit
	return filter, nil
}

func writeAuditEventError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrInvalidAuditEventQuery) {
		JSONError(w, http.StatusBadRequest, "invalid audit event query")

		return
	}
	writeSecurityError(w, err)
}
