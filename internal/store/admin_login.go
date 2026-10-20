package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

type LocalSessionAdmission struct {
	User      model.AdminUser
	Session   model.AdminSession
	Audit     model.AdminAuditEvent
	Bootstrap bool
}

// AdmitLocalSession 原子提交本地会话、登录时间和审计，拒绝验证后已改变的账户。
func (store *AdminStore) AdmitLocalSession(ctx context.Context, admission LocalSessionAdmission) error {
	return store.AdmitLoginSession(ctx, LoginSessionAdmission{Session: admission})
}

func validateLocalAdmission(a LocalSessionAdmission) error {
	s, e := a.Session, a.Audit
	if a.User.ID == "" || !a.User.Enabled || s.ID == "" || s.UserID != a.User.ID || len(s.TokenHash) == 0 ||
		!s.Active(e.CreatedAt) || e.CreatedAt.IsZero() || e.CreatedAt.Before(s.CreatedAt) || s.CreatedAt.IsZero() || s.ReauthenticatedUntil != nil || s.AuthenticationMethod != model.AuthMethodLocal || s.ExternalIdentityID != nil {
		return ErrAdminSessionNotFound
	}
	if e.ID == "" || e.RequestID == "" || e.ActorID != a.User.ID || e.ActorUsername != a.User.Username || e.SessionPrefix != s.TokenPrefix || e.Outcome != model.AuditOutcomeSuccess {
		return ErrAdminSessionNotFound
	}
	if a.Bootstrap {
		if a.User.Role != model.AdminRoleOwner || e.Action != "auth.bootstrap" || e.ResourceType != "administrator" || e.ResourceID != a.User.ID {
			return ErrAdminSessionNotFound
		}
	} else if e.Action != "auth.login" || e.ResourceType != "session" || e.ResourceID != s.ID {
		return ErrAdminSessionNotFound
	}
	return nil
}

func checkLoginUser(ctx context.Context, tx *sql.Tx, expected model.AdminUser) error {
	user, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", expected.ID))
	if err != nil {
		return err
	}
	if user == nil || !user.Enabled || user.Role != expected.Role || user.Username != expected.Username ||
		user.PasswordHash != expected.PasswordHash || !user.PasswordChangedAt.Equal(expected.PasswordChangedAt) ||
		!bytes.Equal(user.TOTPSecretEncrypted, expected.TOTPSecretEncrypted) || !sameOptionalTime(user.TOTPEnabledAt, expected.TOTPEnabledAt) {
		return ErrAdminSessionNotFound
	}
	return nil
}

func checkLocalAdmissionResult(ctx context.Context, tx *sql.Tx, a LocalSessionAdmission) error {
	if err := checkLoginUser(ctx, tx, a.User); err != nil {
		return err
	}
	session, err := scanAdminSession(tx.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id=?", a.Session.ID))
	if err != nil {
		return err
	}
	if session == nil || session.UserID != a.User.ID || !bytes.Equal(session.TokenHash, a.Session.TokenHash) ||
		session.TokenPrefix != a.Session.TokenPrefix || session.RevokedAt != nil || session.ReauthenticatedUntil != nil ||
		session.AuthenticationMethod != model.AuthMethodLocal || session.ExternalIdentityID != nil ||
		!session.CreatedAt.Equal(a.Session.CreatedAt) || !session.LastActivityAt.Equal(a.Session.LastActivityAt) ||
		!session.IdleExpiresAt.Equal(a.Session.IdleExpiresAt) || !session.AbsoluteExpiresAt.Equal(a.Session.AbsoluteExpiresAt) {
		return ErrAdminSessionNotFound
	}
	var loginAt sql.NullTime
	if err := tx.QueryRowContext(ctx, "SELECT last_login_at FROM admin_users WHERE id=?", a.User.ID).Scan(&loginAt); err != nil {
		return err
	}
	if !loginAt.Valid || !loginAt.Time.Equal(a.Session.CreatedAt) {
		return errors.New("login time was not persisted")
	}
	return nil
}
