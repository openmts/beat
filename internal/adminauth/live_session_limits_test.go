package adminauth

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestLiveSessionCapacityAndCleanup(t *testing.T) {
	service := newTestService(t)
	owner := pendingTestOwner(t, service)
	var releases []func()
	for range maximumUserLiveSessions {
		_, release, err := service.GuardSession(t.Context(), owner)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
		releases = append(releases, release)
	}
	if _, _, err := service.GuardSession(t.Context(), owner); !errors.Is(err, ErrLiveSessionCapacity) {
		t.Fatalf("connection limit not enforced: %v", err)
	}
	releases[0]()
	guarded, release, err := service.GuardSession(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
	assertLiveCanceled(t, guarded)
}

func TestLiveSessionGlobalCapacity(t *testing.T) {
	registry := &liveSessionRegistry{sessions: make(map[*liveSession]struct{})}
	for index := range maximumLiveSessions {
		_, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		err := registry.add(&liveSession{cancel: cancel, principal: model.AdminPrincipal{
			User: model.AdminUser{ID: fmt.Sprint(index)},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.add(&liveSession{}); !errors.Is(err, ErrLiveSessionCapacity) {
		t.Fatalf("global connection limit not enforced: %v", err)
	}
}

func TestLiveSessionRejectsMismatchedIdentityAndChangedRole(t *testing.T) {
	service := newTestService(t)
	owner := pendingTestOwner(t, service)
	for _, change := range []string{"missing session", "wrong user", "wrong role", "canceled"} {
		t.Run(change, func(t *testing.T) {
			principal := owner
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch change {
			case "missing session":
				principal.Session.ID = "missing"
			case "wrong user":
				principal.User.ID = "someone-else"
			case "wrong role":
				principal.User.Role = model.AdminRoleAdmin
			case "canceled":
				cancel()
			}
			if _, _, err := service.GuardSession(ctx, principal); !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("invalid live session accepted: %v", err)
			}
		})
	}
}

func TestFailedAccountEditKeepsLiveSession(t *testing.T) {
	service := newTestService(t)
	owner := pendingTestOwner(t, service)
	guarded := liveTestGuard(t, service, owner)
	if err := service.Reauthenticate(t.Context(), &owner, pendingTestPassword, ""); err != nil {
		t.Fatal(err)
	}
	err := service.UpdateUser(t.Context(), &owner, owner.User.ID,
		UpdateUserRequest{DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: false})
	if err == nil || guarded.Err() != nil {
		t.Fatalf("failed edit canceled valid connection: %v / %v", err, guarded.Err())
	}
}
