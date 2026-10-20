package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type RestoredAuthentication struct {
	BackupID string
	At       time.Time
}

// InvalidateRestoredAuthentication 只在尚未启用的恢复副本中撤销会话和临时登录状态。
func InvalidateRestoredAuthentication(ctx context.Context, database *sql.DB, restore RestoredAuthentication) error {
	if database == nil || restore.BackupID == "" || restore.At.IsZero() {
		return errors.New("invalid authentication restore context")
	}
	transaction, err := beginWriteTx(ctx, database)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if err := invalidateRecoverySessions(ctx, transaction, restore.At); err != nil {
		return err
	}
	if err := removeRestoredOIDCTransactions(ctx, transaction); err != nil {
		return err
	}
	event := model.AdminAuditEvent{ID: uuid.NewString(), ActorUsername: "system", Action: "security.restore_sessions_revoked", ResourceType: "backup", ResourceID: restore.BackupID,
		Outcome: model.AuditOutcomeSuccess, DetailJSON: `{}`, CreatedAt: restore.At}
	if err := insertAuditEvent(ctx, transaction, &event); err != nil {
		return err
	}
	return transaction.Commit()
}

func removeRestoredOIDCTransactions(ctx context.Context, transaction *sql.Tx) error {
	var exists bool
	if err := transaction.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='admin_oidc_transactions')`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM admin_oidc_transactions"); err != nil {
		return err
	}
	var remaining int
	if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_oidc_transactions").Scan(&remaining); err != nil {
		return err
	}
	if remaining != 0 {
		return errors.New("restored identity transactions were not invalidated")
	}
	return nil
}
