package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beat/backend/internal/model"
)

// BackupTransition 描述一次带审计归因的状态推进：只有 CAS 成功才写入审计事件。
type BackupTransition struct {
	Record   model.BackupRecord
	Expected []string
	Audit    model.AdminAuditEvent
}

// Transition 在一个事务内推进备份状态并写审计，避免“状态已变但审计缺失”。
func (store *BackupStore) Transition(ctx context.Context, transition BackupTransition) error {
	if len(transition.Expected) == 0 {
		return errors.New("expected backup states are required")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin backup transition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := runBackupTransition(ctx, tx, transition); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit backup transition: %w", err)
	}
	return nil
}

// Audit 记录一次不改变记录行的备份审计事件（例如删除完成）。
func (store *BackupStore) Audit(ctx context.Context, event *model.AdminAuditEvent) error {
	return insertAuditEvent(ctx, store.db, event)
}

// ExpireTerminal 删除超出保留窗口的终态记录，返回删除行数；归档缺失或无效时才可达这些终态。
func (store *BackupStore) ExpireTerminal(
	ctx context.Context,
	states []string,
	cutoff time.Time,
) (int64, error) {
	if len(states) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(states))
	args := []any{cutoff}
	for index, state := range states {
		placeholders[index] = "?"
		args = append(args, state)
	}
	result, err := store.db.ExecContext(ctx, `DELETE FROM admin_backups WHERE created_at < ?
		AND state IN (`+strings.Join(placeholders, ", ")+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("expire terminal backup records: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read expired backup rows: %w", err)
	}
	return count, nil
}

func runBackupTransition(ctx context.Context, tx *sql.Tx, transition BackupTransition) error {
	placeholders := make([]string, len(transition.Expected))
	args := append(backupUpdateArgs(&transition.Record), transition.Record.ID)
	for index, state := range transition.Expected {
		placeholders[index] = "?"
		args = append(args, state)
	}
	result, err := tx.ExecContext(ctx, `UPDATE admin_backups SET `+backupUpdateSet()+
		` WHERE id = ? AND state IN (`+strings.Join(placeholders, ", ")+`)`, args...)
	if err != nil {
		return fmt.Errorf("update backup record state: %w", err)
	}
	if err := requireStateChange(result); err != nil {
		return err
	}
	if transition.Audit.Action == "" {
		return nil
	}
	return insertAuditEvent(ctx, tx, &transition.Audit)
}
