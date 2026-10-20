package adminauth

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

func (service *Service) ListUsers(ctx context.Context, principal *model.AdminPrincipal) ([]model.AdminUser, error) {
	if err := service.RequireOwner(principal); err != nil {
		return nil, err
	}
	return service.config.Store.ListUsers(ctx)
}

func (service *Service) CreateUser(
	ctx context.Context, principal *model.AdminPrincipal, request CreateUserRequest,
) (model.AdminUser, error) {
	if err := service.RequireOwnerRecent(principal); err != nil {
		return model.AdminUser{}, err
	}
	if err := model.ValidateAdminPassword(request.Password); err != nil {
		return model.AdminUser{}, err
	}
	hash, err := service.config.Passwords.Hash(request.Password)
	if err != nil {
		return model.AdminUser{}, err
	}
	now := service.now()
	user := model.AdminUser{ID: uuid.New().String(), Username: request.Username,
		DisplayName: request.DisplayName, Role: request.Role, PasswordHash: hash, Enabled: true,
		PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	user.Normalize()
	if err := user.Validate(); err != nil {
		return model.AdminUser{}, err
	}
	created, err := service.changeAdministrator(ctx, store.AdministratorChange{Principal: *principal, User: user, Audit: model.AdminAuditEvent{Action: store.AdministratorCreate}})
	if err != nil {
		return model.AdminUser{}, err
	}
	return *created, nil
}

func (service *Service) UpdateUser(
	ctx context.Context, principal *model.AdminPrincipal, id string, request UpdateUserRequest,
) error {
	if err := service.RequireOwnerRecent(principal); err != nil {
		return err
	}
	candidate := model.AdminUser{Username: "valid", DisplayName: request.DisplayName, Role: request.Role}
	candidate.Normalize()
	if err := candidate.Validate(); err != nil {
		return err
	}
	candidate.ID, candidate.Enabled = id, request.Enabled
	if _, err := service.changeAdministrator(ctx, store.AdministratorChange{Principal: *principal, User: candidate, Audit: model.AdminAuditEvent{Action: store.AdministratorUpdate}}); err != nil {
		return err
	}
	service.invalidateLiveRole(id, request)
	return nil
}

func (service *Service) DeleteUser(ctx context.Context, principal *model.AdminPrincipal, id string) error {
	if err := service.RequireOwnerRecent(principal); err != nil {
		return err
	}
	if principal.User.ID == id {
		return errors.New("current administrator cannot delete their own account")
	}
	if _, err := service.changeAdministrator(ctx, store.AdministratorChange{Principal: *principal, User: model.AdminUser{ID: id}, Audit: model.AdminAuditEvent{Action: store.AdministratorDelete}}); err != nil {
		return err
	}
	service.revokeLiveUser(id, "")
	return nil
}

func (service *Service) ChangePassword(
	ctx context.Context, principal *model.AdminPrincipal, current, replacement, code string,
) error {
	if err := model.ValidateAdminPassword(replacement); err != nil {
		return err
	}
	if err := service.Reauthenticate(ctx, principal, current, code); err != nil {
		return err
	}
	hash, err := service.config.Passwords.Hash(replacement)
	if err != nil {
		return err
	}
	updated, err := service.changeAdministrator(ctx, store.AdministratorChange{Principal: *principal, User: model.AdminUser{ID: principal.User.ID, PasswordHash: hash}, ExpectedPasswordHash: principal.User.PasswordHash, Audit: model.AdminAuditEvent{Action: store.AdministratorPassword}})
	if err != nil {
		return err
	}
	principal.User = *updated
	service.revokeLiveUser(principal.User.ID, principal.Session.ID)
	return nil
}

func (service *Service) DisableTOTP(ctx context.Context, principal *model.AdminPrincipal) error {
	if principal == nil {
		return ErrSessionInvalid
	}
	if !principal.Session.RecentlyAuthenticated(service.now()) {
		return ErrRecentAuthRequired
	}
	service.pendingTOTP.mu.Lock()
	defer service.pendingTOTP.mu.Unlock()
	snapshot, err := service.selfTOTPSnapshot(ctx, principal)
	if err != nil {
		return err
	}
	err = service.config.Secrets.WithWrite(ctx, func(_ secretbox.ResourceAccess) error {
		_, err := service.commitSelfSecurity(ctx, service.selfSecurityChange(snapshot, store.AdministratorTOTPDisable, snapshot.User.ID))
		return err
	})
	if err != nil {
		return err
	}
	service.pendingTOTP.clearUser(snapshot.User.ID)
	return nil
}

func (service *Service) ListSessions(
	ctx context.Context, principal *model.AdminPrincipal, limit int,
) ([]model.AdminSession, error) {
	sessions, err := service.config.Store.ListSessions(ctx, principal.User.ID, service.now(), limit)
	if err != nil {
		return nil, err
	}
	for index := range sessions {
		sessions[index].Current = sessions[index].ID == principal.Session.ID
	}
	return sessions, nil
}

func (service *Service) RevokeSession(ctx context.Context, principal *model.AdminPrincipal, id string) error {
	if principal == nil {
		return ErrSessionInvalid
	}
	service.pendingTOTP.mu.Lock()
	defer service.pendingTOTP.mu.Unlock()
	_, err := service.commitSelfSecurity(ctx, service.selfSecurityChange(*principal, store.AdministratorSessionRevoke, id))
	if err != nil {
		return err
	}
	service.pendingTOTP.clearSession(id)
	service.revokeLiveSession(id)
	return nil
}

func (service *Service) RevokeOtherSessions(ctx context.Context, principal *model.AdminPrincipal) (int64, error) {
	if principal == nil {
		return 0, ErrSessionInvalid
	}
	service.pendingTOTP.mu.Lock()
	defer service.pendingTOTP.mu.Unlock()
	count, err := service.commitSelfSecurity(ctx, service.selfSecurityChange(*principal, store.AdministratorSessionsRevoke, principal.User.ID))
	if err != nil {
		return 0, err
	}
	service.pendingTOTP.clearOthers(principal.User.ID, principal.Session.ID)
	service.revokeLiveUser(principal.User.ID, principal.Session.ID)
	return count, nil
}

func (service *Service) RequireOwner(principal *model.AdminPrincipal) error {
	if principal == nil || principal.User.Role != model.AdminRoleOwner || !principal.User.Enabled {
		return ErrForbidden
	}
	return nil
}

func (service *Service) RequireOwnerRecent(principal *model.AdminPrincipal) error {
	if err := service.RequireOwner(principal); err != nil {
		return err
	}
	if !principal.Session.RecentlyAuthenticated(service.now()) {
		return ErrRecentAuthRequired
	}
	return nil
}
