package handler

import (
	"errors"
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (handler *AdminAuthHandler) HandleListUsers(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	users, err := handler.service.ListUsers(request.Context(), &principal)
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, users)
}

func (handler *AdminAuthHandler) HandleCreateUser(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	var body struct {
		Username    string          `json:"username"`
		DisplayName string          `json:"display_name"`
		Role        model.AdminRole `json:"role"`
		Password    string          `json:"password"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid administrator request")
		return
	}
	user, err := handler.service.CreateUser(request.Context(), &principal, adminauth.CreateUserRequest{
		Username: body.Username, DisplayName: body.DisplayName, Role: body.Role, Password: body.Password,
	})
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, user)
}

func (handler *AdminAuthHandler) HandleUpdateUser(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	var body struct {
		DisplayName string          `json:"display_name"`
		Role        model.AdminRole `json:"role"`
		Enabled     bool            `json:"enabled"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid administrator request")
		return
	}
	err := handler.service.UpdateUser(request.Context(), &principal, request.PathValue("id"),
		adminauth.UpdateUserRequest{DisplayName: body.DisplayName, Role: body.Role, Enabled: body.Enabled})
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleDeleteUser(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	if err := handler.service.DeleteUser(request.Context(), &principal, request.PathValue("id")); err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *AdminAuthHandler) HandleChangePassword(w http.ResponseWriter, request *http.Request) {
	principal, ok := requirePrincipal(w, request)
	if !ok {
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		TOTPCode        string `json:"totp_code"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid password request")
		return
	}
	if err := handler.service.ChangePassword(request.Context(), &principal,
		body.CurrentPassword, body.NewPassword, body.TOTPCode); err != nil {
		writeSecurityError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func requirePrincipal(w http.ResponseWriter, request *http.Request) (model.AdminPrincipal, bool) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
	}
	return principal, ok
}

func writeSecurityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, adminauth.ErrForbidden):
		JSONError(w, http.StatusForbidden, "owner permission is required")
	case errors.Is(err, adminauth.ErrRecentAuthRequired):
		JSONError(w, http.StatusPreconditionRequired, "recent authentication is required")
	case errors.Is(err, store.ErrLastOwner):
		JSONError(w, http.StatusConflict, "at least one enabled owner is required")
	case errors.Is(err, store.ErrAdminUserNotFound), errors.Is(err, store.ErrAdminSessionNotFound):
		JSONError(w, http.StatusNotFound, "administrator resource was not found")
	case errors.Is(err, adminauth.ErrInvalidCredentials), errors.Is(err, adminauth.ErrInvalidTOTP):
		JSONError(w, http.StatusUnauthorized, "invalid credentials")
	default:
		JSONError(w, http.StatusBadRequest, "administrator request failed")
	}
}
