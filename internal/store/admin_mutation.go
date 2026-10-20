package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/beat/backend/internal/model"
)

var (
	ErrAdminUsernameConflict    = errors.New("administrator username already exists")
	ErrAdminMutationDenied      = errors.New("administrator mutation is not authorized")
	ErrAdminMutationRecent      = errors.New("administrator mutation requires recent identity proof")
	ErrAdminMutationConflict    = errors.New("administrator security state changed")
	ErrAdminMutationUnavailable = errors.New("administrator mutation could not be persisted")
)

const (
	AdministratorCreate   = "admin.user.create"
	AdministratorUpdate   = "admin.user.update"
	AdministratorDelete   = "admin.user.delete"
	AdministratorPassword = "admin.password.change"
)

type AdministratorChange struct {
	Principal            model.AdminPrincipal
	Audit                model.AdminAuditEvent
	User                 model.AdminUser
	ExpectedPasswordHash string
}

func (AdministratorChange) String() string { return "AdministratorChange{redacted}" }

func (change AdministratorChange) GoString() string { return change.String() }

// ChangeAdministrator 提交账户、安全会话副作用及审计的单一事务。
func (store *AdminStore) ChangeAdministrator(ctx context.Context, change AdministratorChange) (*model.AdminUser, error) {
	if err := validateAdministratorEnvelope(change); err != nil {
		return nil, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Join(ErrAdminMutationUnavailable, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := authorizeAdministratorChange(ctx, tx, &change); err != nil {
		return nil, err
	}
	if err := prepareAdministratorAudit(ctx, tx, &change); err != nil {
		return nil, err
	}
	result, err := applyAdministratorChange(ctx, tx, change)
	if err != nil {
		return nil, err
	}
	if err := insertAuditEvent(ctx, tx, &change.Audit); err != nil {
		return nil, errors.Join(ErrAdminMutationUnavailable, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Join(ErrAdminMutationUnavailable, err)
	}
	return result, nil
}

func validateAdministratorEnvelope(change AdministratorChange) error {
	event := change.Audit
	if event.ID == "" || event.RequestID == "" || event.CreatedAt.IsZero() || change.User.ID == "" ||
		event.ActorID != change.Principal.User.ID || event.ResourceType != "administrator" || event.ResourceID != change.User.ID || event.Outcome != model.AuditOutcomeSuccess {
		return ErrAdminMutationDenied
	}
	switch event.Action {
	case AdministratorCreate, AdministratorUpdate, AdministratorDelete:
		return nil
	case AdministratorPassword:
		if change.User.ID == change.Principal.User.ID && change.ExpectedPasswordHash != "" && change.User.PasswordHash != "" {
			return nil
		}
	}
	return ErrAdminMutationDenied
}

func authorizeAdministratorChange(ctx context.Context, tx *sql.Tx, change *AdministratorChange) error {
	session, err := scanAdminSession(tx.QueryRowContext(ctx, "SELECT "+adminSessionColumns+" FROM admin_sessions WHERE id=?", change.Principal.Session.ID))
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	if session == nil || session.UserID != change.Principal.User.ID || !session.Active(change.Audit.CreatedAt) {
		return ErrAdminSessionNotFound
	}
	user, err := scanAdminUser(tx.QueryRowContext(ctx, "SELECT "+adminUserColumns+" FROM admin_users WHERE id=?", session.UserID))
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	if user == nil || !user.Enabled {
		return ErrAdminMutationDenied
	}
	if change.Audit.Action != AdministratorPassword && user.Role != model.AdminRoleOwner {
		return ErrAdminMutationDenied
	}
	if !session.RecentlyAuthenticated(change.Audit.CreatedAt) {
		return ErrAdminMutationRecent
	}
	if change.Audit.Action == AdministratorDelete && change.User.ID == user.ID {
		return ErrAdminMutationDenied
	}
	change.Audit.ActorUsername, change.Audit.SessionPrefix = user.Username, session.TokenPrefix
	return nil
}

func invalidateAdministratorSessions(ctx context.Context, tx *sql.Tx, change AdministratorChange) error {
	keep := ""
	if change.Audit.Action == AdministratorPassword {
		keep = change.Principal.Session.ID
	}
	_, err := tx.ExecContext(ctx, "UPDATE admin_sessions SET revoked_at=?,reauthenticated_until=NULL WHERE user_id=? AND id<>? AND revoked_at IS NULL", change.Audit.CreatedAt, change.User.ID, keep)
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	var remaining int
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_sessions WHERE user_id=? AND id<>? AND revoked_at IS NULL", change.User.ID, keep).Scan(&remaining)
	if err != nil || remaining != 0 {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	return nil
}

func ensureTransactionalOwner(ctx context.Context, tx *sql.Tx, change AdministratorChange) error {
	var count int
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admin_users WHERE role='owner' AND enabled=1 AND id<>?", change.User.ID).Scan(&count)
	if err != nil {
		return errors.Join(ErrAdminMutationUnavailable, err)
	}
	if count == 0 {
		return ErrLastOwner
	}
	return nil
}

func administratorMutationTime(change AdministratorChange) time.Time {
	return change.Audit.CreatedAt.UTC()
}
