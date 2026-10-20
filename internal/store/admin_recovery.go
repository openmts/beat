package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

const MaximumSupportedSchemaVersion = currentSchemaVersion

var ErrRecoverySnapshotChanged = errors.New("the inspected recovery state changed")

type AdminRecoveryWrite struct {
	UserID, Username, PasswordHash   string
	SecretGeneration, RootGeneration int64
	At                               time.Time
}

func (AdminRecoveryWrite) String() string { return "administrator recovery write" }

func (AdminRecoveryWrite) GoString() string { return "administrator recovery write" }

// RecoverOwner 只供持有离线生命周期锁的本机恢复流程调用，不开放HTTP入口。
func (store *AdminStore) RecoverOwner(ctx context.Context, write AdminRecoveryWrite) error {
	if write.UserID == "" || write.Username == "" || write.PasswordHash == "" || write.At.IsZero() {
		return ErrRecoverySnapshotChanged
	}
	transaction, err := beginWriteTx(ctx, store.db)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := checkRecoveryGeneration(ctx, transaction, write); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE admin_users SET password_hash=?,password_changed_at=?,enabled=1,
		totp_secret_encrypted=NULL,totp_enabled_at=NULL,updated_at=? WHERE id=? AND username=? AND role='owner'`,
		write.PasswordHash, write.At, write.At, write.UserID, write.Username)
	changed, err := eventMutationResult(result, err)
	if err != nil {
		return err
	}
	if !changed {
		return ErrRecoverySnapshotChanged
	}
	if err := invalidateRecoverySessions(ctx, transaction, write.At); err != nil {
		return err
	}
	if err := removeRestoredOIDCTransactions(ctx, transaction); err != nil {
		return err
	}
	if err := recordOwnerRecovery(ctx, transaction, write); err != nil {
		return err
	}
	return transaction.Commit()
}

func recordOwnerRecovery(ctx context.Context, transaction *sql.Tx, write AdminRecoveryWrite) error {
	detail, err := json.Marshal(struct {
		Username string   `json:"target_username"`
		Actions  []string `json:"actions"`
	}{Username: write.Username, Actions: []string{"password_reset", "totp_removed", "owner_enabled", "all_sessions_revoked", "pending_identity_transactions_removed"}})
	if err != nil {
		return err
	}
	return insertAuditEvent(ctx, transaction, &model.AdminAuditEvent{ID: uuid.NewString(), ActorUsername: "local_operator", Action: "security.break_glass_recovery",
		ResourceType: "admin_user", ResourceID: write.UserID, Outcome: model.AuditOutcomeSuccess, DetailJSON: string(detail), CreatedAt: write.At})
}

func checkRecoveryGeneration(ctx context.Context, transaction *sql.Tx, write AdminRecoveryWrite) error {
	var valid bool
	err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM application_secret_state WHERE id=1 AND phase='ready' AND generation=? AND root_generation=?)
		AND (SELECT MAX(version) FROM schema_migrations)=?`, write.SecretGeneration, write.RootGeneration, currentSchemaVersion).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrRecoverySnapshotChanged
	}
	return nil
}

func invalidateRecoverySessions(ctx context.Context, transaction *sql.Tx, at time.Time) error {
	if _, err := transaction.ExecContext(ctx, `UPDATE admin_sessions SET revoked_at=COALESCE(revoked_at,?),reauthenticated_until=NULL`, at); err != nil {
		return err
	}
	var active int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_sessions WHERE revoked_at IS NULL OR reauthenticated_until IS NOT NULL`).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return errors.New("administrator sessions were not invalidated")
	}
	return nil
}
