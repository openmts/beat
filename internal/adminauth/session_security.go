package adminauth

import (
	"context"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (service *Service) commitSessionSecurity(ctx context.Context, principal model.AdminPrincipal, until *time.Time) error {
	action := "auth.logout"
	if until != nil {
		action = "auth.reauthenticate"
	}
	event := service.auditEvent(ctx, AuditInput{Principal: &principal, Action: action, ResourceType: "session", ResourceID: principal.Session.ID, Outcome: model.AuditOutcomeSuccess})
	return service.config.Store.CommitSessionSecurity(ctx, store.SessionSecurityChange{Principal: principal, Audit: event, ReauthenticatedUntil: until})
}
