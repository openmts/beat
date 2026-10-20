package adminauth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	maximumLiveSessions      = 32
	maximumUserLiveSessions  = 4
	liveSessionCheckInterval = time.Second
)

var ErrLiveSessionCapacity = errors.New("administrator connection limit reached")

type liveSession struct {
	principal model.AdminPrincipal
	cancel    context.CancelFunc
}

type liveSessionRegistry struct {
	mu       sync.Mutex
	sessions map[*liveSession]struct{}
}

// GuardSession 将长连接绑定到有效会话，不以后台校验延长空闲期限。
func (service *Service) GuardSession(ctx context.Context, principal model.AdminPrincipal) (context.Context, func(), error) {
	guarded, cancel := context.WithCancel(ctx)
	session := &liveSession{principal: principal, cancel: cancel}
	if err := service.live.add(session); err != nil {
		cancel()
		return nil, nil, err
	}
	if err := service.validateLiveSession(guarded, principal); err != nil {
		cancel()
		service.live.remove(session)
		return nil, nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer service.live.remove(session)
		defer cancel()
		service.monitorLiveSession(guarded, principal)
	}()
	return guarded, func() { cancel(); <-done }, nil
}

func (service *Service) monitorLiveSession(ctx context.Context, principal model.AdminPrincipal) {
	ticker := time.NewTicker(service.liveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := service.validateLiveSession(ctx, principal); err != nil {
				return
			}
		}
	}
}

func (service *Service) validateLiveSession(ctx context.Context, principal model.AdminPrincipal) error {
	session, err := service.config.Store.GetSessionByID(ctx, principal.Session.ID)
	if err != nil || session == nil || session.UserID != principal.User.ID || !session.Active(service.now()) {
		return ErrSessionInvalid
	}
	user, err := service.config.Store.GetUserByID(ctx, session.UserID)
	if err != nil || user == nil || !user.Enabled || user.Role != principal.User.Role {
		return ErrSessionInvalid
	}
	return nil
}

func (registry *liveSessionRegistry) add(session *liveSession) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	userCount := 0
	for active := range registry.sessions {
		if active.principal.User.ID == session.principal.User.ID {
			userCount++
		}
	}
	if len(registry.sessions) >= maximumLiveSessions || userCount >= maximumUserLiveSessions {
		return ErrLiveSessionCapacity
	}
	registry.sessions[session] = struct{}{}
	return nil
}

func (registry *liveSessionRegistry) remove(session *liveSession) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.sessions, session)
}

func (registry *liveSessionRegistry) revoke(matches func(model.AdminPrincipal) bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for session := range registry.sessions {
		if matches(session.principal) {
			session.cancel()
		}
	}
}

func (service *Service) revokeLiveSession(id string) {
	service.live.revoke(func(principal model.AdminPrincipal) bool { return principal.Session.ID == id })
}

func (service *Service) revokeLiveUser(userID, exceptSessionID string) {
	service.live.revoke(func(principal model.AdminPrincipal) bool {
		return principal.User.ID == userID && principal.Session.ID != exceptSessionID
	})
}

func (service *Service) invalidateLiveRole(userID string, request UpdateUserRequest) {
	service.live.revoke(func(principal model.AdminPrincipal) bool {
		return principal.User.ID == userID && (!request.Enabled || principal.User.Role != request.Role)
	})
}
