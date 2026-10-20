package operatorrecovery

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"syscall"

	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/store"
)

func pendingRestore(dataDir string) (bool, error) { return backup.PendingRestore(dataDir) }

// inspectMigrationDatabase 在私有副本上检查完整性、账本与待应用迁移。
func inspectMigrationDatabase(ctx context.Context, config Config, report MigrationReport) MigrationReport {
	temporary, err := os.MkdirTemp(filepath.Dir(config.DataDir), ".beat-migration-status-")
	if err != nil {
		return failedMigrationReport(report, migrationCodeUnreadable)
	}
	defer func() { _ = os.RemoveAll(temporary) }()
	snapshot := filepath.Join(temporary, "beat.db")
	if err := copySnapshot(config.DatabasePath, snapshot); err != nil {
		return failedMigrationReport(report, migrationCodeUnreadable)
	}
	database, err := openDatabase(snapshot, false)
	if err != nil {
		return failedMigrationReport(report, migrationCodeUnreadable)
	}
	defer func() { _ = database.Close() }()
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return failedMigrationReport(report, migrationCodeUnreadable)
	}
	if integrity != "ok" {
		return failedMigrationReport(report, migrationCodeIntegrityFailed)
	}
	report.Integrity = true
	ledger, err := readMigrationLedger(ctx, database)
	if err != nil {
		return failedMigrationReport(report, migrationCodeUnreadable)
	}
	report.MissingLedger = ledger.missing
	if ledger.missing || ledger.invalid {
		report.CurrentSchema = ledger.maximum
		return failedMigrationReport(report, migrationCodeLedgerInvalid)
	}
	if ledger.maximum > store.MaximumSupportedSchemaVersion {
		report.CurrentSchema, report.FutureSchema = ledger.maximum, true
		return failedMigrationReport(report, migrationCodeFutureSchema)
	}
	report.CurrentSchema = ledger.maximum
	report.PendingMigrations = pendingMigrationSteps(ledger.maximum)
	switch {
	case report.PendingRestore:
		report.State, report.Code = "pending_restore", migrationCodePendingRestore
	case len(report.PendingMigrations) > 0:
		report.State, report.Code = "upgrade_pending", migrationCodeOK
	}
	return report
}

func failedMigrationReport(report MigrationReport, code string) MigrationReport {
	report.State, report.Code = "failed", code
	return report
}

type migrationLedger struct {
	maximum int
	missing bool
	invalid bool
}

// readMigrationLedger 校验账本连续、名称规范，并区分缺失与损坏。
func readMigrationLedger(ctx context.Context, database *sql.DB) (migrationLedger, error) {
	var ledger migrationLedger
	rows, err := database.QueryContext(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		ledger.missing = true
		return ledger, nil
	}
	defer func() { _ = rows.Close() }()
	entries := make([]MigrationStep, 0, store.MaximumSupportedSchemaVersion)
	for rows.Next() {
		var entry MigrationStep
		if err := rows.Scan(&entry.Version, &entry.Name); err != nil {
			return ledger, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return ledger, err
	}
	return classifyLedger(entries), nil
}

func classifyLedger(entries []MigrationStep) migrationLedger {
	var ledger migrationLedger
	if len(entries) == 0 {
		ledger.missing = true
		return ledger
	}
	inventory := store.MigrationInventory()
	for index, entry := range entries {
		if entry.Version > ledger.maximum {
			ledger.maximum = entry.Version
		}
		// 比本二进制更新的版本由调用方归类为 future_schema，而不是账本损坏。
		if entry.Version > len(inventory) {
			continue
		}
		if entry.Version < 1 || entry.Version != index+1 || entry.Name != inventory[entry.Version-1].Name {
			ledger.invalid = true
		}
	}
	return ledger
}

func ownedByUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
