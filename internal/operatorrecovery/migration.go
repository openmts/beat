package operatorrecovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/store"
)

// 迁移状态固定代码：命令只输出这些值，绝不回传底层错误文本。
const (
	migrationCodeOK              = "ok"
	migrationCodeDatabaseAbsent  = "database_absent"
	migrationCodeLockConflict    = "lock_conflict"
	migrationCodePendingRestore  = "pending_restore"
	migrationCodeRestoreUnknown  = "restore_state_unknown"
	migrationCodeInsecureMode    = "insecure_mode"
	migrationCodeUnreadable      = "database_unreadable"
	migrationCodeIntegrityFailed = "integrity_failed"
	migrationCodeLedgerInvalid   = "ledger_invalid"
	migrationCodeFutureSchema    = "future_schema"
	maximumPendingMigrations     = 64
)

// MigrationStep 是报告里的单条待应用迁移，只含版本号与规范名称。
type MigrationStep struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
}

// MigrationReport 是 `admin-recovery migration-status` 的唯一输出。
// 它不含 SQL、表内容、行 ID、密钥、DSN、MTS 路径或归档内容。
type MigrationReport struct {
	LastFailure            *migrationstatus.Marker `json:"last_failure,omitempty"`
	State                  string                  `json:"state"`
	Code                   string                  `json:"code"`
	PendingMigrations      []MigrationStep         `json:"pending_migrations"`
	CurrentSchema          int                     `json:"current_schema"`
	MinimumSupportedSchema int                     `json:"minimum_supported_schema"`
	MaximumSupportedSchema int                     `json:"maximum_supported_schema"`
	BackupArchiveCount     int                     `json:"backup_archive_count"`
	DatabaseMode           string                  `json:"database_mode"`
	DatabasePresent        bool                    `json:"database_present"`
	Integrity              bool                    `json:"integrity"`
	MissingLedger          bool                    `json:"missing_ledger"`
	FutureSchema           bool                    `json:"future_schema"`
	PendingRestore         bool                    `json:"pending_restore"`
	LockConflict           bool                    `json:"lock_conflict"`
	BackupArchivePresent   bool                    `json:"backup_archive_present"`
	DatabaseOwnedByUser    bool                    `json:"database_owned_by_user"`
}

func runMigrationStatus(options commandOptions, output io.Writer) error {
	config, err := resolveConfig(options.config)
	if err != nil {
		return errors.New("recovery phase=status result=failed code=instance_validation")
	}
	if err := validateDirectory(config.DataDir); err != nil {
		return errors.New("recovery phase=status result=failed code=private_directory")
	}
	return json.NewEncoder(output).Encode(collectMigrationStatus(context.Background(), config))
}

// collectMigrationStatus 只读取私有副本；存在锁冲突时不再读取实时数据库。
func collectMigrationStatus(ctx context.Context, config Config) MigrationReport {
	report := MigrationReport{State: "current", Code: migrationCodeOK, PendingMigrations: []MigrationStep{},
		MinimumSupportedSchema: store.MinimumSupportedSchemaVersion, MaximumSupportedSchema: store.MaximumSupportedSchemaVersion}
	if marker, err := migrationstatus.Read(config.DataDir); err == nil {
		report.LastFailure = &marker
	}
	restore, restoreCode := pendingRestoreState(config.DataDir)
	report.PendingRestore, report.Code = restore, restoreCode
	if restoreCode == migrationCodeRestoreUnknown {
		report.State = "failed"
		return report
	}
	report.BackupArchivePresent, report.BackupArchiveCount = backupArchives(config.DataDir)
	lease, err := applicationsecrets.AcquireLease(config.DataDir)
	if errors.Is(err, applicationsecrets.ErrLifecycleBusy) {
		report.State, report.Code, report.LockConflict = "locked", migrationCodeLockConflict, true
		return report
	}
	if err != nil {
		report.State, report.Code = "failed", migrationCodeUnreadable
		return report
	}
	defer func() { _ = lease.Close() }()
	info, err := os.Lstat(config.DatabasePath)
	if errors.Is(err, os.ErrNotExist) {
		return absentDatabaseReport(report)
	} else if err != nil {
		report.State, report.Code = "failed", migrationCodeUnreadable
		return report
	}
	report.DatabasePresent = true
	report.DatabaseMode = fmt.Sprintf("%04o", info.Mode().Perm())
	report.DatabaseOwnedByUser = ownedByUser(info)
	if err := privatePath(config.DatabasePath, false); err != nil {
		report.State, report.Code = "failed", migrationCodeInsecureMode
		return report
	}
	return inspectMigrationDatabase(ctx, config, report)
}

// pendingRestoreState 区分“没有待恢复”与“无法判定恢复状态”。
func pendingRestoreState(dataDir string) (bool, string) {
	pending, err := pendingRestore(dataDir)
	if err != nil {
		return false, migrationCodeRestoreUnknown
	}
	if pending {
		return true, migrationCodePendingRestore
	}
	return false, migrationCodeOK
}

func absentDatabaseReport(report MigrationReport) MigrationReport {
	if report.PendingRestore {
		report.State, report.Code = "restore_staged", migrationCodePendingRestore
		return report
	}
	report.State, report.Code = "empty", migrationCodeDatabaseAbsent
	return report
}

func backupArchives(dataDir string) (bool, int) {
	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil {
		return false, 0
	}
	count := 0
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			count++
		}
	}
	return count > 0, count
}

func pendingMigrationSteps(source int) []MigrationStep {
	pending := store.PendingMigrations(source)
	steps := make([]MigrationStep, 0, len(pending))
	for index, migration := range pending {
		if index == maximumPendingMigrations {
			break
		}
		steps = append(steps, MigrationStep{Version: migration.Version, Name: migration.Name})
	}
	return steps
}
