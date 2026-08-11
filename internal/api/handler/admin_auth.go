package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
)

const (
	AdminSessionCookieName = "beat_admin_session"
	maximumAuthBody        = 16 << 10
)

type AdminAuthHandler struct {
	service *adminauth.Service
}

func NewAdminAuthHandler(service *adminauth.Service) *AdminAuthHandler {
	return &AdminAuthHandler{service: service}
}

func (handler *AdminAuthHandler) HandleState(w http.ResponseWriter, request *http.Request) {
	state, err := handler.service.State(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "authentication state is unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, state)
}

func (handler *AdminAuthHandler) HandleBootstrap(w http.ResponseWriter, request *http.Request) {
	var body struct {
		BootstrapToken string `json:"bootstrap_token"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
		Password       string `json:"password"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid authentication request")
		return
	}
	result, err := handler.service.Bootstrap(request.Context(), adminauth.BootstrapRequest{
		BootstrapToken: body.BootstrapToken, Username: body.Username, DisplayName: body.DisplayName,
		Password: body.Password, IPAddress: remoteIP(request), UserAgent: request.UserAgent(),
	})
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	setAdminSessionCookie(w, request, result.Token, result.Principal.Session.AbsoluteExpiresAt)
	JSONResponse(w, http.StatusCreated, result.Principal)
}

func (handler *AdminAuthHandler) HandleLogin(w http.ResponseWriter, request *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid authentication request")
		return
	}
	result, err := handler.service.Login(request.Context(), adminauth.LoginRequest{
		Username: body.Username, Password: body.Password, TOTPCode: body.TOTPCode,
		IPAddress: remoteIP(request), UserAgent: request.UserAgent(),
	})
	if err != nil {
		writeAuthenticationError(w, err)
		return
	}
	setAdminSessionCookie(w, request, result.Token, result.Principal.Session.AbsoluteExpiresAt)
	JSONResponse(w, http.StatusOK, result.Principal)
}

func (handler *AdminAuthHandler) HandleSession(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	JSONResponse(w, http.StatusOK, principal)
}

func (handler *AdminAuthHandler) HandleLogout(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := handler.service.Logout(request.Context(), &principal); err != nil {
		JSONError(w, http.StatusInternalServerError, "logout failed")
		return
	}
	expireAdminSessionCookie(w, request)
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleReauthenticate(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid authentication request")
		return
	}
	if err := handler.service.Reauthenticate(request.Context(), &principal, body.Password, body.TOTPCode); err != nil {
		writeAuthenticationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, principal.Session)
}

func decodeAuthBody(w http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(w, request.Body, maximumAuthBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("authentication request contains trailing data")
	}
	return nil
}

func writeAuthenticationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, adminauth.ErrLocked):
		JSONError(w, http.StatusTooManyRequests, "too many login attempts")
	case errors.Is(err, adminauth.ErrBootstrapComplete):
		JSONError(w, http.StatusConflict, "administrator setup is already complete")
	case errors.Is(err, adminauth.ErrTOTPRequired):
		JSONError(w, http.StatusPreconditionRequired, "TOTP code is required")
	case errors.Is(err, adminauth.ErrInvalidCredentials), errors.Is(err, adminauth.ErrInvalidTOTP),
		errors.Is(err, adminauth.ErrBootstrapDenied):
		JSONError(w, http.StatusUnauthorized, "invalid credentials")
	default:
		JSONError(w, http.StatusBadRequest, "authentication request failed")
	}
}

func setAdminSessionCookie(w http.ResponseWriter, request *http.Request, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: AdminSessionCookieName, Value: value, Path: "/",
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true,
		Secure: middleware.IsSecure(request), SameSite: http.SameSiteStrictMode})
}

func expireAdminSessionCookie(w http.ResponseWriter, request *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: AdminSessionCookieName, Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: middleware.IsSecure(request),
		SameSite: http.SameSiteStrictMode})
}

func remoteIP(request *http.Request) string {
	return middleware.ClientIP(request)
}
