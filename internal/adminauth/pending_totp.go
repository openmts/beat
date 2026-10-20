package adminauth

import (
	"context"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	pendingTOTPLifetime        = 5 * time.Minute
	maximumPendingTOTP         = 256
	maximumPendingTOTPFailures = 5
)

type pendingTOTP struct {
	userID                 string
	secret, previousSecret []byte
	previousEnabledAt      *time.Time
	expiresAt              time.Time
	failures               int
}

type pendingTOTPStore struct {
	mu      sync.Mutex
	entries map[string]pendingTOTP
}

func newPendingTOTPStore() *pendingTOTPStore {
	return &pendingTOTPStore{entries: make(map[string]pendingTOTP)}
}

func (pending *pendingTOTPStore) prune(now time.Time) {
	for sessionID, setup := range pending.entries {
		if !now.Before(setup.expiresAt) {
			clear(setup.secret)
			delete(pending.entries, sessionID)
		}
	}
}

func (pending *pendingTOTPStore) clearUser(userID string) {
	for sessionID, setup := range pending.entries {
		if setup.userID == userID {
			clear(setup.secret)
			delete(pending.entries, sessionID)
		}
	}
}

// 使用数据库中的会话状态，避免过期或撤销前缓存的 principal 继续确认设置。
func (service *Service) totpUser(ctx context.Context, principal *model.AdminPrincipal) (*model.AdminUser, error) {
	if principal == nil || principal.User.ID == "" || principal.Session.ID == "" {
		return nil, ErrSessionInvalid
	}
	session, err := service.config.Store.GetSessionByID(ctx, principal.Session.ID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.UserID != principal.User.ID || !session.Active(service.now()) {
		return nil, ErrSessionInvalid
	}
	if !session.RecentlyAuthenticated(service.now()) {
		return nil, ErrRecentAuthRequired
	}
	user, err := service.config.Store.GetUserByID(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.Enabled {
		return nil, ErrSessionInvalid
	}
	return user, nil
}
