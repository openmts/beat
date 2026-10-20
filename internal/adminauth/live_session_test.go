package adminauth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestLiveSessionRevocationIsSelective(t *testing.T) {
	for _, operation := range []string{"logout", "revoke", "others", "password"} {
		t.Run(operation, func(t *testing.T) {
			service := newTestService(t)
			owner := pendingTestOwner(t, service)
			other := liveTestLogin(t, service)
			first := liveTestGuard(t, service, owner)
			second := liveTestGuard(t, service, other)
			var err error
			switch operation {
			case "logout":
				err = service.Logout(t.Context(), &other)
			case "revoke":
				err = service.RevokeSession(t.Context(), &owner, other.Session.ID)
			case "others":
				_, err = service.RevokeOtherSessions(t.Context(), &owner)
			case "password":
				err = service.ChangePassword(t.Context(), &owner, pendingTestPassword, "a replacement secure password", "")
			}
			if err != nil {
				t.Fatal(err)
			}
			assertLiveCanceled(t, second)
			if err := first.Err(); err != nil {
				t.Fatalf("unaffected session closed: %v", err)
			}
		})
	}
}

func TestLiveSessionAccountLifecycle(t *testing.T) {
	for _, change := range []string{"disable", "delete", "role", "display"} {
		t.Run(change, func(t *testing.T) {
			service := newTestService(t)
			owner := pendingTestOwner(t, service)
			operator := liveTestOperator(t, service, owner)
			guarded := liveTestGuard(t, service, operator)
			if err := service.Reauthenticate(t.Context(), &owner, pendingTestPassword, ""); err != nil {
				t.Fatal(err)
			}
			request := UpdateUserRequest{DisplayName: "Updated", Role: model.AdminRoleAdmin, Enabled: true}
			if change == "disable" {
				request.Enabled = false
			}
			if change == "role" {
				request.Role = model.AdminRoleOwner
			}
			var err error
			if change == "delete" {
				err = service.DeleteUser(t.Context(), &owner, operator.User.ID)
			} else {
				err = service.UpdateUser(t.Context(), &owner, operator.User.ID, request)
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "display" {
				if guarded.Err() != nil {
					t.Fatal("display name edit closed terminal")
				}
				return
			}
			assertLiveCanceled(t, guarded)
		})
	}
}

func TestLiveSessionExpiryDoesNotRefreshIdleLifetime(t *testing.T) {
	service := newTestService(t)
	owner := pendingTestOwner(t, service)
	service.liveInterval = time.Millisecond
	var clock atomic.Int64
	clock.Store(service.Now().Unix())
	service.now = func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	guarded := liveTestGuard(t, service, owner)
	clock.Store(owner.Session.IdleExpiresAt.Add(time.Second).Unix())
	assertLiveCanceled(t, guarded)
	stored, err := service.config.Store.GetSessionByID(t.Context(), owner.Session.ID)
	if err != nil || !stored.LastActivityAt.Equal(owner.Session.LastActivityAt) || !stored.IdleExpiresAt.Equal(owner.Session.IdleExpiresAt) {
		t.Fatalf("background checks extended session lifetime: %v", err)
	}
}

func TestLiveSessionDetectsExternalRevocation(t *testing.T) {
	service := newTestService(t)
	owner := pendingTestOwner(t, service)
	service.liveInterval = time.Millisecond
	guarded := liveTestGuard(t, service, owner)
	if err := service.config.Store.RevokeSession(t.Context(), owner.Session.ID, service.Now()); err != nil {
		t.Fatal(err)
	}
	assertLiveCanceled(t, guarded)
	if _, _, err := service.GuardSession(t.Context(), owner); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session admitted: %v", err)
	}
}

func liveTestLogin(t *testing.T, service *Service) model.AdminPrincipal {
	t.Helper()
	result, err := service.Login(t.Context(), LoginRequest{Username: "owner", Password: pendingTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	return result.Principal
}

func liveTestOperator(t *testing.T, service *Service, owner model.AdminPrincipal) model.AdminPrincipal {
	if err := service.Reauthenticate(t.Context(), &owner, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	t.Helper()
	_, err := service.CreateUser(t.Context(), &owner, CreateUserRequest{
		Username: "operator", DisplayName: "Operator", Role: model.AdminRoleAdmin, Password: pendingTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(t.Context(), LoginRequest{Username: "operator", Password: pendingTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	return result.Principal
}

func liveTestGuard(t *testing.T, service *Service, principal model.AdminPrincipal) context.Context {
	t.Helper()
	guarded, release, err := service.GuardSession(t.Context(), principal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return guarded
}

func assertLiveCanceled(t *testing.T, guarded context.Context) {
	t.Helper()
	select {
	case <-guarded.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("live administrator connection survived invalidation")
	}
}
