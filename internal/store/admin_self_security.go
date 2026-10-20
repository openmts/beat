package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	AdministratorTOTPConfigure  = "admin.totp.configure"
	AdministratorSessionRevoke  = "admin.session.revoke"
	AdministratorSessionsRevoke = "admin.sessions.revoke_others"
	AdministratorTOTPBegin      = "admin.totp.begin"
	AdministratorTOTPEnable     = "admin.totp.enable"
	AdministratorTOTPDisable    = "admin.totp.disable"
)

type SelfSecurityChange struct {
	Principal              model.AdminPrincipal
	Audit                  model.AdminAuditEvent
	Secret, PreviousSecret []byte
	PreviousEnabledAt      *time.Time
}

func (SelfSecurityChange) String() string { return "SelfSecurityChange{redacted}" }

func (change SelfSecurityChange) GoString() string { return change.String() }

// CommitSelfSecurity 复用统一审计写入器，在同一事务内提交本人会话和因子操作。
func (store *AdminStore) CommitSelfSecurity(ctx context.Context, change SelfSecurityChange) (int64, error) {
	if err := validateSelfSecurity(change); err != nil {
		return 0, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = authorizeSelfSecurity(ctx, tx, change); err != nil {
		return 0, err
	}
	count, err := applySelfSecurity(ctx, tx, change)
	if err != nil {
		return 0, err
	}
	change.Audit.DetailJSON, err = selfSecurityDetail(change, count)
	if err != nil {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	if err = insertAuditEvent(ctx, tx, &change.Audit); err != nil {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	if err = tx.Commit(); err != nil {
		return 0, errors.Join(ErrAdminMutationUnavailable, err)
	}
	return count, nil
}

func validateSelfSecurity(change SelfSecurityChange) error {
	event := change.Audit
	if event.ID == "" || event.RequestID == "" || event.CreatedAt.IsZero() || event.ActorID != change.Principal.User.ID || event.Outcome != model.AuditOutcomeSuccess {
		return ErrAdminMutationDenied
	}
	switch event.Action {
	case AdministratorSessionRevoke:
		if event.ResourceType == "session" && event.ResourceID != "" {
			return nil
		}
	case AdministratorSessionsRevoke, AdministratorTOTPBegin, AdministratorTOTPDisable, AdministratorTOTPEnable:
		if event.ResourceType != "administrator" || event.ResourceID != change.Principal.User.ID {
			return ErrAdminMutationDenied
		}
		if event.Action != AdministratorTOTPEnable || len(change.Secret) > 0 {
			return nil
		}
	}
	return ErrAdminMutationDenied
}

func authorizeSelfSecurity(ctx context.Context, tx *sql.Tx, change SelfSecurityChange) error {
	snapshot := SessionSecurityChange{Principal: change.Principal, Audit: change.Audit}
	recent := change.Audit.Action != AdministratorSessionRevoke && change.Audit.Action != AdministratorSessionsRevoke
	if recent && change.Audit.Action != AdministratorTOTPEnable {
		snapshot.ReauthenticatedUntil = &change.Audit.CreatedAt
	}
	if err := checkSessionSecuritySnapshot(ctx, tx, snapshot); err != nil {
		if errors.Is(err, ErrAdminSessionNotFound) {
			return errors.Join(ErrAdminMutationDenied, err)
		}
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	if recent {
		session, err := scanAdminSession(tx.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id=?", change.Principal.Session.ID))
		if err != nil {
			return errors.Join(ErrAdminMutationUnavailable, err)
		}
		if session == nil || !session.RecentlyAuthenticated(change.Audit.CreatedAt) {
			return ErrAdminMutationRecent
		}
	}
	return nil
}
