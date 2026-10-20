package adminauth

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (service *Service) changeAdministrator(ctx context.Context, change store.AdministratorChange) (*model.AdminUser, error) {
	change.Audit = service.auditEvent(ctx, AuditInput{Principal: &change.Principal, Action: change.Audit.Action, ResourceType: "administrator", ResourceID: change.User.ID, Outcome: model.AuditOutcomeSuccess})
	user, err := service.config.Store.ChangeAdministrator(ctx, change)
	switch {
	case errors.Is(err, store.ErrAdminMutationDenied):
		return nil, ErrForbidden
	case errors.Is(err, store.ErrAdminMutationRecent):
		return nil, ErrRecentAuthRequired
	case errors.Is(err, store.ErrAdminSessionNotFound):
		return nil, ErrSessionInvalid
	}
	return user, err
}
