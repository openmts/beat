package adminauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
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
	if err := service.RequireOwner(principal); err != nil {
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
	if err := service.config.Store.CreateUser(ctx, &user); err != nil {
		return model.AdminUser{}, err
	}
	return user, nil
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
	return service.config.Store.UpdateUser(ctx, id, candidate.DisplayName, candidate.Role, request.Enabled)
}

func (service *Service) DeleteUser(ctx context.Context, principal *model.AdminPrincipal, id string) error {
	if err := service.RequireOwnerRecent(principal); err != nil {
		return err
	}
	if principal.User.ID == id {
		return errors.New("current administrator cannot delete their own account")
	}
	return service.config.Store.DeleteUser(ctx, id)
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
	now := service.now()
	if err := service.config.Store.UpdatePassword(ctx, principal.User.ID, hash, now); err != nil {
		return err
	}
	if err := service.config.Store.RevokeUserSessions(ctx, principal.User.ID, principal.Session.ID, now); err != nil {
		return fmt.Errorf("revoke sessions after password change: %w", err)
	}
	return nil
}

func (service *Service) DisableTOTP(ctx context.Context, principal *model.AdminPrincipal) error {
	if !principal.Session.RecentlyAuthenticated(service.now()) {
		return ErrRecentAuthRequired
	}
	return service.config.Store.UpdateTOTP(ctx, principal.User.ID, nil, nil)
}

func (service *Service) ListSessions(
	ctx context.Context, principal *model.AdminPrincipal,
) ([]model.AdminSession, error) {
	sessions, err := service.config.Store.ListSessions(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	for index := range sessions {
		sessions[index].Current = sessions[index].ID == principal.Session.ID
	}
	return sessions, nil
}

func (service *Service) RevokeSession(
	ctx context.Context, principal *model.AdminPrincipal, id string,
) error {
	session, err := service.config.Store.GetSessionByID(ctx, id)
	if err != nil {
		return err
	}
	if session == nil || session.UserID != principal.User.ID {
		return store.ErrAdminSessionNotFound
	}
	return service.config.Store.RevokeSession(ctx, id, service.now())
}

func (service *Service) RevokeOtherSessions(ctx context.Context, principal *model.AdminPrincipal) (int64, error) {
	return service.config.Store.RevokeOtherSessions(ctx, principal.User.ID, principal.Session.ID, service.now())
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
