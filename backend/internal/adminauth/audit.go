package adminauth

import (
	"context"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

func (service *Service) RecordAudit(ctx context.Context, input AuditInput) error {
	requestID := input.RequestID
	if requestID == "" {
		requestID = uuid.NewString()
	}
	event := model.AdminAuditEvent{
		ID: uuid.NewString(), RequestID: requestID, Action: input.Action,
		ResourceType: input.ResourceType, ResourceID: input.ResourceID, Outcome: input.Outcome,
		DetailJSON: "{}", IPAddress: input.IPAddress, UserAgent: input.UserAgent,
		CreatedAt: service.now(),
	}
	if input.Principal != nil {
		event.ActorID = input.Principal.User.ID
		event.ActorUsername = input.Principal.User.Username
		event.SessionPrefix = input.Principal.Session.TokenPrefix
	}
	return service.config.Store.CreateAuditEvent(ctx, &event)
}

func (service *Service) ListAuditEvents(
	ctx context.Context, principal *model.AdminPrincipal, filter model.AuditFilter,
) (model.AuditPage, error) {
	if principal == nil {
		return model.AuditPage{}, ErrForbidden
	}
	return service.config.Store.ListAuditEvents(ctx, filter)
}
