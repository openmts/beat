package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"beat/internal/auth"
)

const adminSessionCookie = "beat_admin_session"

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(adminSessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录管理后台")
			return
		}
		session, valid, err := s.authManager.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			s.logger.Error("validate_admin_session_failed", "error", err)
			writeError(w, http.StatusInternalServerError, "auth_error", "登录状态校验失败")
			return
		}
		if !valid {
			s.clearAdminCookie(w, r)
			writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录")
			return
		}
		identity := adminIdentity{Username: session.Username, CSRFToken: session.CSRFToken}
		if !s.requireCSRF(w, r, identity) {
			return
		}
		ctx := context.WithValue(r.Context(), adminIdentityKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	w.Header().Set("Cache-Control", "no-store")
	if s.loginLimiter == nil {
		s.loginLimiter = newLoginLimiter()
	}
	remote := clientAddress(r)
	allowed, retryAfter := s.loginLimiter.allow(remote, time.Now())
	if !allowed {
		w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
		s.auditResponse(r, "admin_login_rate_limited", "", http.StatusTooManyRequests, nil)
		writeError(w, http.StatusTooManyRequests, "login_rate_limited", "登录尝试过于频繁，请稍后再试")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "登录信息格式无效")
		return
	}
	credentials, valid, err := s.authManager.Authenticate(r.Context(), input.Username, input.Password)
	if err != nil {
		s.logger.Error("authenticate_admin_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "auth_error", "登录校验失败")
		return
	}
	if !valid {
		s.loginLimiter.failure(remote, time.Now())
		s.auditResponse(r, "admin_login_failed", strings.TrimSpace(input.Username), http.StatusUnauthorized, nil)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "账号或密码错误")
		return
	}
	s.loginLimiter.success(remote)
	session, err := s.authManager.IssueSession(credentials.Username, credentials.Version)
	if err != nil {
		s.logger.Error("issue_admin_session_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "auth_error", "无法创建登录会话")
		return
	}
	http.SetCookie(w, s.adminCookie(r, session.Token, session.ExpiresAt))
	s.logger.Info("admin_login_succeeded", "username", credentials.Username)
	s.auditResponse(r, "admin_login_succeeded", credentials.Username, http.StatusOK, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"username":      credentials.Username,
		"csrfToken":     session.CSRFToken,
		"expiresAt":     session.ExpiresAt,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if cookie, err := r.Cookie(adminSessionCookie); err == nil {
		session, valid, validationErr := s.authManager.ValidateSession(r.Context(), cookie.Value)
		if validationErr != nil {
			s.logger.Error("validate_admin_session_failed", "error", validationErr)
			writeError(w, http.StatusInternalServerError, "auth_error", "登录状态校验失败")
			return
		}
		if valid && !s.requireCSRF(w, r, adminIdentity{Username: session.Username, CSRFToken: session.CSRFToken}) {
			return
		}
		s.authManager.RevokeSession(cookie.Value)
		if valid {
			s.auditResponse(r, "admin_logout", session.Username, http.StatusOK, nil)
		}
	}
	s.clearAdminCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{"authenticated": false}
	cookie, err := r.Cookie(adminSessionCookie)
	if errors.Is(err, http.ErrNoCookie) {
		writeJSON(w, http.StatusOK, response)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	session, valid, err := s.authManager.ValidateSession(r.Context(), cookie.Value)
	if err != nil {
		s.logger.Error("validate_admin_session_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "auth_error", "登录状态校验失败")
		return
	}
	if !valid {
		s.clearAdminCookie(w, r)
		writeJSON(w, http.StatusOK, response)
		return
	}
	response["authenticated"] = true
	response["username"] = session.Username
	response["csrfToken"] = session.CSRFToken
	response["expiresAt"] = session.ExpiresAt
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleUpdateCredentials(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		Username        string `json:"username"`
		NewPassword     string `json:"newPassword"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "账号与密码配置格式无效")
		return
	}
	credentials, err := s.authManager.UpdateCredentials(r.Context(), input.CurrentPassword, input.Username, input.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, "current_password_invalid", "当前密码不正确")
		case errors.Is(err, auth.ErrInvalidUsername):
			writeError(w, http.StatusBadRequest, "invalid_username", "登录账号不能为空、不能包含空白字符，且最多 64 个字符")
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeError(w, http.StatusBadRequest, "password_too_short", "新密码至少需要 8 个字符")
		case errors.Is(err, auth.ErrPasswordTooLong):
			writeError(w, http.StatusBadRequest, "password_too_long", "新密码不能超过 128 个字符")
		case errors.Is(err, auth.ErrNoChanges):
			writeError(w, http.StatusBadRequest, "credentials_unchanged", "账号和密码均未发生变化")
		default:
			s.logger.Error("update_admin_credentials_failed", "error", err)
			writeError(w, http.StatusInternalServerError, "storage_error", "账号与密码保存失败")
		}
		return
	}
	session, err := s.authManager.IssueSession(credentials.Username, credentials.Version)
	if err != nil {
		s.logger.Error("issue_admin_session_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "auth_error", "凭据已保存，但无法续期登录会话")
		return
	}
	http.SetCookie(w, s.adminCookie(r, session.Token, session.ExpiresAt))
	s.logger.Info("admin_credentials_updated", "username", credentials.Username)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"username":      credentials.Username,
		"csrfToken":     session.CSRFToken,
		"updatedAt":     credentials.UpdatedAt,
		"expiresAt":     session.ExpiresAt,
	})
}

func (s *Server) adminCookie(r *http.Request, token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	}
}

func (s *Server) clearAdminCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(proto, "https")
}
