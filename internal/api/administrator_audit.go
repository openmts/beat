package api

import (
	"net/http"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func administratorAuditPolicy(request *http.Request, principal *model.AdminPrincipal) (string, string, bool) {
	if action, _, id := atomicRouteAuditPolicy(request); action != "" {
		return action, id, true
	}
	switch request.Pattern {
	case "POST /api/v1/admin/users":
		return store.AdministratorCreate, "", true
	case "PUT /api/v1/admin/users/{id}":
		return store.AdministratorUpdate, request.PathValue("id"), true
	case "DELETE /api/v1/admin/users/{id}":
		return store.AdministratorDelete, request.PathValue("id"), true
	case "DELETE /api/v1/admin/sessions/{id}":
		return store.AdministratorSessionRevoke, request.PathValue("id"), true
	case "DELETE /api/v1/admin/sessions/others":
		return store.AdministratorSessionsRevoke, principal.User.ID, true
	case "DELETE /api/v1/admin/users/me/totp":
		return store.AdministratorTOTPDisable, principal.User.ID, true
	case "POST /api/v1/admin/users/me/totp":
		return store.AdministratorTOTPConfigure, principal.User.ID, true
	case "PUT /api/v1/admin/users/me/password":
		return store.AdministratorPassword, principal.User.ID, true
	}
	return "", "", false
}

type requestAuditDecision struct {
	action, resourceID string
	atomic             bool
	status             int
}

func administratorRequestAudit(request *http.Request, principal *model.AdminPrincipal, decision requestAuditDecision) adminauth.AuditInput {
	outcome := model.AuditOutcomeSuccess
	if decision.status >= http.StatusBadRequest {
		outcome = model.AuditOutcomeFailure
	}
	input := adminauth.AuditInput{RequestID: middleware.RequestID(request.Context()), Principal: principal, Action: auditAction(request), ResourceType: "http_route", ResourceID: request.Pattern, Outcome: outcome, IPAddress: requestIP(request), UserAgent: request.UserAgent()}
	if decision.atomic {
		input.Action, input.ResourceType, input.ResourceID = decision.action, "administrator", decision.resourceID
		if _, kind, _ := atomicRouteAuditPolicy(request); kind != "" {
			input.ResourceType = kind
		}
		if decision.action == store.AdministratorSessionRevoke {
			input.ResourceType = "session"
		}
	}
	return input
}
