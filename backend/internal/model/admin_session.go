package model

import "time"

type AdminSession struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"user_id"`
	TokenHash            []byte     `json:"-"`
	TokenPrefix          string     `json:"token_prefix"`
	CreatedAt            time.Time  `json:"created_at"`
	LastActivityAt       time.Time  `json:"last_activity_at"`
	IdleExpiresAt        time.Time  `json:"idle_expires_at"`
	AbsoluteExpiresAt    time.Time  `json:"absolute_expires_at"`
	ReauthenticatedUntil *time.Time `json:"reauthenticated_until"`
	IPAddress            string     `json:"ip_address"`
	UserAgent            string     `json:"user_agent"`
	RevokedAt            *time.Time `json:"revoked_at"`
	Current              bool       `json:"current,omitempty"`
}

func (session AdminSession) Active(now time.Time) bool {
	return session.RevokedAt == nil && now.Before(session.IdleExpiresAt) && now.Before(session.AbsoluteExpiresAt)
}

func (session AdminSession) RecentlyAuthenticated(now time.Time) bool {
	return session.ReauthenticatedUntil != nil && now.Before(*session.ReauthenticatedUntil)
}

type AdminPrincipal struct {
	User    AdminUser    `json:"user"`
	Session AdminSession `json:"session"`
}
