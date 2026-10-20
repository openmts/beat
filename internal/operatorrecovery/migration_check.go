package operatorrecovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/beat/backend/internal/applicationsecrets"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/migrationstatus"
	"github.com/beat/backend/internal/store"
)

// 迁移预检的固定代码与容量下限；它们只允许在这些枚举内取值。
const (
	checkCodeArchiveMissing   = "archive_unreadable"
	checkCodeArchiveInvalid   = "archive_incompatible"
	checkCodeInsufficientRoom = "insufficient_space"
	checkCodeSchemaMismatch   = "schema_mismatch"
	checkCodeMigrationFailed  = "migration_failed"
	minimumCheckFreeBytes     = 256 << 20
	checkStagingPrefix        = ".beat-migration-check-"
)

// MigrationCheckReport 是 `admin-recovery migration-check` 的唯一输出。
// 它不含 SQL、表内容、行 ID、密钥、DSN、私有路径、摘要或归档内容。
type MigrationCheckReport struct {
	LastFailure            *migrationstatus.Marker `json:"last_failure,omitempty"`
	State                  string                  `json:"state"`
	Code                   string                  `json:"code"`
	PendingMigrations      []MigrationStep         `json:"pending_migrations"`
	SourceSchema           int                     `json:"source_schema"`
	TargetSchema           int                     `json:"target_schema"`
	MinimumSupportedSchema int                     `json:"minimum_supported_schema"`
	MaximumSupportedSchema int                     `json:"maximum_supported_schema"`
	ArchiveFormat          int                     `json:"archive_format"`
	ArchiveMetricRows      int64                   `json:"archive_metric_rows"`
	FreeSpaceBytes         uint64                  `json:"free_space_bytes"`
	RequiredFreeBytes      uint64                  `json:"required_free_space_bytes"`
	ArchivePresent         bool                    `json:"archive_present"`
	LiveDatabasePresent    bool                    `json:"live_database_present"`
	LiveDatabaseUnchanged  bool                    `json:"live_database_unchanged"`
	PendingRestore         bool                    `json:"pending_restore"`
	LockConflict           bool                    `json:"lock_conflict"`
	Integrity              bool                    `json:"integrity"`
	ForeignKeys            bool                    `json:"foreign_keys"`
	LedgerValid            bool                    `json:"ledger_valid"`
	SchemaEquivalent       bool                    `json:"schema_equivalent"`
	StagingRemoved         bool                    `json:"staging_removed"`
	Eligible               bool                    `json:"eligible"`
}

// runMigrationCheck 只读校验归档、来源 schema、升级路径与容量下限，绝不修改实时数据库。
func runMigrationCheck(options commandOptions, output io.Writer) error {
	config, err := resolveConfig(options.config)
	if err != nil {
		return errors.New("recovery phase=check result=failed code=instance_validation")
	}
	if err := validateDirectory(config.DataDir); err != nil {
		return errors.New("recovery phase=check result=failed code=private_directory")
	}
	report := checkMigrationArchive(context.Background(), config, options.archive)
	encodeErr := json.NewEncoder(output).Encode(report)
	if report.LockConflict {
		return errors.Join(encodeErr, errors.New("recovery phase=check result=failed code=lock_conflict"))
	}
	if !report.Eligible {
		return errors.Join(encodeErr, errors.New("recovery phase=check result=failed code="+report.Code))
	}
	return encodeErr
}

func checkMigrationArchive(ctx context.Context, config Config, archive string) MigrationCheckReport {
	report := MigrationCheckReport{State: "checked", Code: migrationCodeOK, PendingMigrations: []MigrationStep{},
		MinimumSupportedSchema: store.MinimumSupportedSchemaVersion, MaximumSupportedSchema: store.MaximumSupportedSchemaVersion}
	if marker, err := migrationstatus.Read(config.DataDir); err == nil {
		report.LastFailure = &marker
	}
	restore, restoreCode := pendingRestoreState(config.DataDir)
	report.PendingRestore = restore
	if restoreCode == migrationCodeRestoreUnknown {
		return failedCheckReport(report, migrationCodeRestoreUnknown)
	}
	if report.PendingRestore {
		return failedCheckReport(report, migrationCodePendingRestore)
	}
	lease, err := applicationsecrets.AcquireLease(config.DataDir)
	if errors.Is(err, applicationsecrets.ErrLifecycleBusy) {
		report = failedCheckReport(report, migrationCodeLockConflict)
		report.State, report.LockConflict = "locked", true
		return report
	}
	if err != nil {
		return failedCheckReport(report, migrationCodeUnreadable)
	}
	defer func() { _ = lease.Close() }()
	archiveInfo, err := os.Stat(archive)
	if err != nil || !archiveInfo.Mode().IsRegular() {
		return failedCheckReport(report, checkCodeArchiveMissing)
	}
	report.ArchivePresent = true
	free, err := checkFreeSpace(config.DataDir)
	if err != nil {
		return failedCheckReport(report, migrationCodeUnreadable)
	}
	report.FreeSpaceBytes, report.RequiredFreeBytes = free, requiredCheckFreeBytes(archiveInfo.Size())
	liveBefore, liveStatErr := statFingerprint(config.DatabasePath)
	report.LiveDatabasePresent = liveStatErr == nil
	if free < report.RequiredFreeBytes {
		return failedCheckReport(report, checkCodeInsufficientRoom)
	}
	staging, err := os.MkdirTemp(filepath.Dir(config.DataDir), checkStagingPrefix)
	if err != nil {
		return failedCheckReport(report, migrationCodeUnreadable)
	}
	result := applyMigrationCheck(ctx, config, archive, staging, report, liveBefore)
	result.StagingRemoved = os.RemoveAll(staging) == nil
	if !result.StagingRemoved && result.Eligible {
		return failedCheckReport(result, migrationCodeUnreadable)
	}
	return result
}

// applyMigrationCheck 在私有解包副本上重放规范迁移，并比对升级后与全新库的 schema 签名。
func applyMigrationCheck(ctx context.Context, config Config, archive, staging string, report MigrationCheckReport, liveBefore string) MigrationCheckReport {
	preflight, err := backup.InspectArchive(ctx, archive, staging)
	if err != nil {
		return failedCheckReport(report, checkCodeArchiveInvalid)
	}
	defer func() { _ = preflight.Close() }()
	report.ArchiveFormat, report.ArchiveMetricRows = preflight.FormatVersion, preflight.MetricRows
	sourceSchema, err := inspectArchivedLedger(ctx, preflight.DatabasePath)
	if err != nil {
		return failedCheckReport(report, errCode(err, migrationCodeLedgerInvalid))
	}
	report.SourceSchema, report.TargetSchema = sourceSchema, store.MaximumSupportedSchemaVersion
	report.PendingMigrations = pendingMigrationSteps(sourceSchema)
	if sourceSchema < store.MinimumSupportedSchemaVersion || sourceSchema > store.MaximumSupportedSchemaVersion {
		return failedCheckReport(report, migrationCodeFutureSchema)
	}
	report.LedgerValid = true
	scratch := filepath.Join(staging, "upgraded.db")
	if err := copySnapshot(preflight.DatabasePath, scratch); err != nil {
		return failedCheckReport(report, migrationCodeUnreadable)
	}
	upgraded, err := store.NewSQLiteStoreContext(ctx, scratch)
	if err != nil {
		return failedCheckReport(report, checkCodeMigrationFailed)
	}
	verifyErr := verifyUpgradedSchema(ctx, upgraded.DB, staging)
	closeErr := upgraded.Close()
	if verifyErr != nil || closeErr != nil {
		return failedCheckReport(report, checkCodeSchemaMismatch)
	}
	report.Integrity, report.ForeignKeys, report.SchemaEquivalent = true, true, true
	liveAfter, afterErr := statFingerprint(config.DatabasePath)
	report.LiveDatabaseUnchanged = afterErr == nil && liveAfter == liveBefore
	report.Eligible = report.LiveDatabaseUnchanged
	if !report.Eligible {
		return failedCheckReport(report, migrationCodeUnreadable)
	}
	report.State, report.Code = "eligible", migrationCodeOK
	return report
}

// verifyUpgradedSchema 校验升级副本的完整性与全新库 schema 等价性。
func verifyUpgradedSchema(ctx context.Context, database *sql.DB, staging string) error {
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("upgraded copy failed its integrity check")
	}
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	violated := rows.Next()
	closeErr := rows.Close()
	if violated || closeErr != nil {
		return errors.New("upgraded copy holds foreign key violations")
	}
	if err := store.ValidateSnapshotSchema(ctx, database); err != nil {
		return err
	}
	upgradedInventory, err := schemaInventory(ctx, database)
	if err != nil {
		return err
	}
	return compareFreshSchema(ctx, upgradedInventory, staging)
}

// compareFreshSchema 在私有暂存目录里建全新库，并比对 schema 清单。
func compareFreshSchema(ctx context.Context, upgraded string, staging string) error {
	fresh, err := store.NewSQLiteStoreContext(ctx, filepath.Join(staging, "fresh.db"))
	if err != nil {
		return err
	}
	inventory, inventoryErr := schemaInventory(ctx, fresh.DB)
	closeErr := fresh.Close()
	if inventoryErr != nil || closeErr != nil {
		return errors.New("fresh schema inventory is unavailable")
	}
	if inventory != upgraded {
		return errors.New("upgraded schema differs from a fresh database")
	}
	return nil
}

// schemaInventory 返回规范顺序的表、索引、视图与触发器名称清单。
// 只比较清单而不是 DDL 文本：v9 之后的运行期密钥清洗会合法地改写 ssh_keys 等表的列。
func schemaInventory(ctx context.Context, database *sql.DB) (string, error) {
	rows, err := database.QueryContext(ctx,
		"SELECT type, name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name")
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	var builder strings.Builder
	for rows.Next() {
		var kind, name string
		if err := rows.Scan(&kind, &name); err != nil {
			return "", err
		}
		builder.WriteString(kind + "/" + name + "\n")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return builder.String(), nil
}

// inspectArchivedLedger 读取归档 SQLite 副本的账本连续性；不修改副本。
func inspectArchivedLedger(ctx context.Context, path string) (int, error) {
	database, err := openDatabase(path, false)
	if err != nil {
		return 0, err
	}
	defer func() { _ = database.Close() }()
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return 0, errArchivedIntegrity
	}
	ledger, err := readMigrationLedger(ctx, database)
	if err != nil {
		return 0, err
	}
	if ledger.missing || ledger.invalid {
		return ledger.maximum, errArchivedLedger
	}
	return ledger.maximum, nil
}

var errArchivedIntegrity = errors.New("archived database failed its integrity check")

var errArchivedLedger = errors.New("archived database ledger is invalid")

// errCode 把内部错误映射到固定代码；只有账本类错误保留自带代码。
func errCode(err error, fallback string) string {
	if errors.Is(err, errArchivedLedger) {
		return migrationCodeLedgerInvalid
	}
	if errors.Is(err, errArchivedIntegrity) {
		return migrationCodeIntegrityFailed
	}
	return fallback
}

func failedCheckReport(report MigrationCheckReport, code string) MigrationCheckReport {
	report.State, report.Code, report.Eligible = "failed", code, false
	return report
}

// statFingerprint 只比较大小、修改时间与 inode，绝不读取或打开实时数据库内容。
func statFingerprint(path string) (string, error) {
	var info unix.Stat_t
	if err := unix.Lstat(path, &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", info.Size, info.Mtim.Sec, info.Ino), nil
}

func checkFreeSpace(path string) (uint64, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return 0, err
	}
	return fs.Bavail * uint64(fs.Bsize), nil
}

// requiredCheckFreeBytes 要求 2 倍归档体积加上固定下限，覆盖解包、迁移副本与 WAL。
func requiredCheckFreeBytes(archiveBytes int64) uint64 {
	size := uint64(0)
	if archiveBytes > 0 {
		size = uint64(archiveBytes)
	}
	return size*2 + minimumCheckFreeBytes
}
