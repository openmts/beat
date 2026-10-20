package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/applicationsecrets"
)

type ApplicationSecretOperations interface {
	Status() applicationsecrets.Status
	StartOwnerRotation(context.Context, applicationsecrets.RotationActor) (applicationsecrets.Status, error)
}

type ApplicationSecretHandler struct{ secrets ApplicationSecretOperations }

func NewApplicationSecretHandler(secrets ApplicationSecretOperations) *ApplicationSecretHandler {
	return &ApplicationSecretHandler{secrets: secrets}
}

func (handler *ApplicationSecretHandler) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	JSONResponse(w, http.StatusOK, handler.secrets.Status())
}

func (handler *ApplicationSecretHandler) HandleRotate(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	principal, exists := middleware.AdminPrincipal(request.Context())
	if !exists {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return
	}
	status, err := handler.secrets.StartOwnerRotation(request.Context(), applicationsecrets.RotationActor{
		UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(request.Context()),
		IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent(),
	})
	if err != nil {
		code := http.StatusServiceUnavailable
		if errors.Is(err, applicationsecrets.ErrLifecycleBusy) || errors.Is(err, applicationsecrets.ErrKeyLimit) {
			code = http.StatusConflict
		}
		if errors.Is(err, applicationsecrets.ErrRotationForbidden) {
			code = http.StatusForbidden
		}
		if errors.Is(err, applicationsecrets.ErrRotationRecentAuth) {
			code = http.StatusPreconditionRequired
		}
		JSONError(w, code, "application secret rotation is unavailable")
		return
	}
	JSONResponse(w, http.StatusAccepted, status)
}
