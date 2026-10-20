package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
)

// OIDCSessionAdmission 绑定已验证的提供方配置、身份和本地用户快照。
type OIDCSessionAdmission struct {
	Provider     model.OIDCProvider
	Identity     model.ExternalIdentity
	User         model.AdminUser
	Session      model.AdminSession
	Audit        model.AdminAuditEvent
	Events       []model.SecurityEvent
	Fingerprints []SecurityFingerprint
}

// AdmitOIDCSession 一次提交会话、两侧登录时间及成功审计；任何失败全部回滚。
func (store *AdminStore) AdmitOIDCSession(ctx context.Context, admission OIDCSessionAdmission) error {
	if err := validateOIDCAdmission(admission); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := store.AdmitOIDCSessionTx(ctx, tx, admission); err != nil {
		return err
	}
	return tx.Commit()
}

// AdmitOIDCSessionTx 供通知队列事务回调使用。
func (store *AdminStore) AdmitOIDCSessionTx(
	ctx context.Context, tx *sql.Tx, admission OIDCSessionAdmission,
) error {
	if err := validateOIDCAdmission(admission); err != nil {
		return err
	}
	if err := checkOIDCAdmissionAuthority(ctx, tx, admission); err != nil {
		return err
	}
	if err := insertOIDCSession(ctx, tx, admission.Session); err != nil {
		return err
	}
	if err := touchOIDCLogin(ctx, tx, admission); err != nil {
		return err
	}
	if err := insertAuditEvent(ctx, tx, &admission.Audit); err != nil {
		return err
	}
	if err := insertSecurityEvents(ctx, tx, admission.Events); err != nil {
		return err
	}
	if err := upsertSecurityFingerprints(ctx, tx, admission.Fingerprints); err != nil {
		return err
	}
	return checkOIDCAdmissionResult(ctx, tx, admission)
}

func validateOIDCAdmission(a OIDCSessionAdmission) error {
	s, e := a.Session, a.Audit
	if a.User.ID == "" || !a.User.Enabled || a.Provider.ID == "" || !a.Provider.Enabled || a.Provider.ConfigurationVersion < 1 ||
		a.Identity.ID == "" || a.Identity.Subject == "" || a.Identity.UserID != a.User.ID || a.Identity.ProviderID != a.Provider.ID {
		return ErrAdminSessionNotFound
	}
	if s.ID == "" || s.UserID != a.User.ID || len(s.TokenHash) == 0 || s.CreatedAt.IsZero() || !s.Active(e.CreatedAt) ||
		e.CreatedAt.Before(s.CreatedAt) || s.ReauthenticatedUntil != nil || s.AuthenticationMethod != model.AuthMethodOIDC ||
		s.ExternalIdentityID == nil || *s.ExternalIdentityID != a.Identity.ID {
		return ErrAdminSessionNotFound
	}
	if e.ID == "" || e.RequestID == "" || e.ActorID != a.User.ID || e.ActorUsername != a.User.Username || e.SessionPrefix != s.TokenPrefix ||
		e.Outcome != model.AuditOutcomeSuccess || e.Action != "auth.oidc.login" || e.ResourceType != "provider" || e.ResourceID != a.Provider.ID {
		return ErrAdminSessionNotFound
	}
	return nil
}

func touchOIDCLogin(ctx context.Context, tx *sql.Tx, a OIDCSessionAdmission) error {
	result, err := tx.ExecContext(ctx, "UPDATE admin_users SET last_login_at=? WHERE id=?", a.Session.CreatedAt, a.User.ID)
	if err := sessionRowsAffected(result, err, "update OIDC user login time"); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx, "UPDATE admin_external_identities SET last_login_at=?,updated_at=? WHERE id=?", a.Session.CreatedAt, a.Session.CreatedAt, a.Identity.ID)
	return sessionRowsAffected(result, err, "update OIDC identity login time")
}

func insertOIDCSession(ctx context.Context, tx *sql.Tx, s model.AdminSession) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_sessions (
 id,user_id,token_hash,token_prefix,created_at,last_activity_at,idle_expires_at,absolute_expires_at,
 reauthenticated_until,ip_address,user_agent,revoked_at,authentication_method,external_identity_id
 ) VALUES(?,?,?,?,?,?,?,?,NULL,?,?,NULL,?,?)`, s.ID, s.UserID, s.TokenHash, s.TokenPrefix, s.CreatedAt, s.LastActivityAt, s.IdleExpiresAt, s.AbsoluteExpiresAt, s.IPAddress, s.UserAgent, model.AuthMethodOIDC, s.ExternalIdentityID)
	return sessionRowsAffected(result, err, "insert OIDC session")
}

func checkOIDCLoginTimes(ctx context.Context, tx *sql.Tx, a OIDCSessionAdmission) error {
	var userAt, identityAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT u.last_login_at,e.last_login_at FROM admin_users u JOIN admin_external_identities e ON e.user_id=u.id WHERE u.id=? AND e.id=?`, a.User.ID, a.Identity.ID).Scan(&userAt, &identityAt)
	if err != nil {
		return err
	}
	if !userAt.Valid || !identityAt.Valid || !userAt.Time.Equal(a.Session.CreatedAt) || !identityAt.Time.Equal(a.Session.CreatedAt) {
		return errors.New("OIDC login times were not persisted")
	}
	return nil
}
