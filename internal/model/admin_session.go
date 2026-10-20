package model

import "time"

const (
	// MaxActiveAdminSessions 是每用户活跃会话上限：成功登录时在同一事务内吊销最旧的多余会话。
	MaxActiveAdminSessions = 32
	// AdminSessionPageSize 与 AdminSessionMaximumPage 约束会话列表的返回上限。
	AdminSessionPageSize    = 50
	AdminSessionMaximumPage = 200
	// AdminSessionRecentRevokedWindow 让界面仍能看到刚刚被吊销的会话，而不是只剩活跃会话。
	AdminSessionRecentRevokedWindow = 24 * time.Hour
)

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
	AuthenticationMethod string     `json:"authentication_method"`
	ExternalIdentityID   *string    `json:"external_identity_id,omitempty"`
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
