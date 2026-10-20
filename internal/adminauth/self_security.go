package adminauth

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (service *Service) selfSecurityChange(principal model.AdminPrincipal, action, target string) store.SelfSecurityChange {
	resource := "administrator"
	if action == store.AdministratorSessionRevoke {
		resource = "session"
	}
	return store.SelfSecurityChange{Principal: principal, Audit: model.AdminAuditEvent{Action: action, ResourceType: resource, ResourceID: target}}
}

func (service *Service) commitSelfSecurity(ctx context.Context, change store.SelfSecurityChange) (int64, error) {
	change.Audit = service.auditEvent(ctx, AuditInput{Principal: &change.Principal, Action: change.Audit.Action, ResourceType: change.Audit.ResourceType, ResourceID: change.Audit.ResourceID, Outcome: model.AuditOutcomeSuccess})
	count, err := service.config.Store.CommitSelfSecurity(ctx, change)
	switch {
	case errors.Is(err, store.ErrAdminMutationDenied):
		return 0, ErrSessionInvalid
	case errors.Is(err, store.ErrAdminMutationRecent):
		return 0, ErrRecentAuthRequired
	case errors.Is(err, store.ErrAdminMutationConflict):
		return 0, ErrTOTPSetupExpired
	}
	return count, err
}

// 调用方持有 pendingTOTP.mu，只有持久化成功后才清除对应设置。
func (pending *pendingTOTPStore) clearSession(sessionID string) {
	if setup, exists := pending.entries[sessionID]; exists {
		clear(setup.secret)
		delete(pending.entries, sessionID)
	}
}

func (pending *pendingTOTPStore) clearOthers(userID, currentID string) {
	for id, setup := range pending.entries {
		if setup.userID == userID && id != currentID {
			pending.clearSession(id)
		}
	}
}

func (service *Service) selfTOTPSnapshot(ctx context.Context, principal *model.AdminPrincipal) (model.AdminPrincipal, error) {
	user, err := service.totpUser(ctx, principal)
	if err != nil {
		return model.AdminPrincipal{}, err
	}
	if user.Role != principal.User.Role {
		return model.AdminPrincipal{}, ErrSessionInvalid
	}
	snapshot := *principal
	snapshot.User = *user
	return snapshot, nil
}
