package handler

import (
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func (handler *AdminAuthHandler) HandleTOTP(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid TOTP request")
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
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleDisableTOTP(w http.ResponseWriter, request *http.Request) {
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
	sessions, err := handler.service.ListSessions(request.Context(), &principal)
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, sessions)
}

func (handler *AdminAuthHandler) HandleRevokeSession(w http.ResponseWriter, request *http.Request) {
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
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	filter := model.AuditFilter{Action: request.URL.Query().Get("action"),
		ActorID: request.URL.Query().Get("actor_id"), Limit: boundedQueryInt(request, "limit", 50),
		Offset: boundedQueryInt(request, "offset", 0)}
	page, err := handler.service.ListAuditEvents(request.Context(), &principal, filter)
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, page)
}

func boundedQueryInt(request *http.Request, key string, fallback int) int {
	value := request.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
