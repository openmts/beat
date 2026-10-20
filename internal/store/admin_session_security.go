package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

type SessionSecurityChange struct {
	Principal            model.AdminPrincipal
	Audit                model.AdminAuditEvent
	ReauthenticatedUntil *time.Time
}

// CommitSessionSecurity 将会话变更和审计原子提交，并复查验证期间的安全状态。
func (store *AdminStore) CommitSessionSecurity(ctx context.Context, change SessionSecurityChange) error {
	if err := validateSessionSecurityChange(change); err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := checkSessionSecuritySnapshot(ctx, tx, change); err != nil {
		return err
	}
	var result sql.Result
	if change.ReauthenticatedUntil == nil {
		result, err = tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=?, reauthenticated_until=NULL WHERE id=? AND revoked_at IS NULL", change.Audit.CreatedAt, change.Principal.Session.ID)
	} else {
		result, err = tx.ExecContext(ctx, "UPDATE admin_sessions SET reauthenticated_until=? WHERE id=? AND revoked_at IS NULL", *change.ReauthenticatedUntil, change.Principal.Session.ID)
	}
	if err := sessionRowsAffected(result, err, "commit session security"); err != nil {
		return err
	}
	if err := checkSessionSecurityResult(ctx, tx, change); err != nil {
		return err
	}
	if err := insertAuditEvent(ctx, tx, &change.Audit); err != nil {
		return err
	}
	return tx.Commit()
}

func validateSessionSecurityChange(change SessionSecurityChange) error {
	event := change.Audit
	if event.ID == "" || event.RequestID == "" || event.CreatedAt.IsZero() || event.ResourceType != "session" ||
		event.ResourceID != change.Principal.Session.ID || event.ActorID != change.Principal.User.ID ||
		event.Outcome != model.AuditOutcomeSuccess {
		return ErrAdminSessionNotFound
	}
	if change.ReauthenticatedUntil == nil {
		if event.Action != "auth.logout" {
			return ErrAdminSessionNotFound
		}
	} else if event.Action != "auth.reauthenticate" || !change.ReauthenticatedUntil.After(event.CreatedAt) {
		return ErrAdminSessionNotFound
	}
	return nil
}

func checkSessionSecuritySnapshot(ctx context.Context, tx *sql.Tx, change SessionSecurityChange) error {
	session, err := scanAdminSession(tx.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id=?", change.Principal.Session.ID))
	if err != nil {
		return err
	}
	if session == nil || session.UserID != change.Principal.User.ID || !session.Active(change.Audit.CreatedAt) {
		return ErrAdminSessionNotFound
	}
	user, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", session.UserID))
	if err != nil {
		return err
	}
	if user == nil || !user.Enabled || user.Role != change.Principal.User.Role {
		return ErrAdminSessionNotFound
	}
	if user.Username != change.Audit.ActorUsername || session.TokenPrefix != change.Audit.SessionPrefix {
		return ErrAdminSessionNotFound
	}
	if change.ReauthenticatedUntil != nil && (user.PasswordHash != change.Principal.User.PasswordHash ||
		!bytes.Equal(user.TOTPSecretEncrypted, change.Principal.User.TOTPSecretEncrypted) ||
		!sameOptionalTime(user.TOTPEnabledAt, change.Principal.User.TOTPEnabledAt)) {
		return ErrAdminSessionNotFound
	}
	return nil
}

func checkSessionSecurityResult(ctx context.Context, tx *sql.Tx, change SessionSecurityChange) error {
	session, err := scanAdminSession(tx.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id=?", change.Principal.Session.ID))
	if err != nil {
		return err
	}
	if session == nil {
		return ErrAdminSessionNotFound
	}
	if change.ReauthenticatedUntil == nil {
		if !sameOptionalTime(session.RevokedAt, &change.Audit.CreatedAt) || session.ReauthenticatedUntil != nil {
			return errors.New("logout state was not persisted")
		}
	} else if !sameOptionalTime(session.ReauthenticatedUntil, change.ReauthenticatedUntil) || session.RevokedAt != nil {
		return errors.New("identity proof was not persisted")
	}
	return nil
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
