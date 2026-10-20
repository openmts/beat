package store

import (
	"errors"
	"os"
	"strings"
)

// 迁移失败分类是固定枚举：只有这些值可以进入状态标记、日志与运维输出。
const (
	MigrationFailureFutureSchema = "future_schema"
	MigrationFailureLedger       = "ledger"
	MigrationFailureApply        = "apply"
	MigrationFailureBusy         = "busy"
	MigrationFailurePermission   = "permission"
	MigrationFailureUnknown      = "unknown"
)

// MinimumSupportedSchemaVersion 是迁移检查接受的最低来源 schema 版本。
const MinimumSupportedSchemaVersion = 1

// Migration 描述一条按应用顺序排列的规范 SQLite 迁移。
type Migration struct {
	Version int
	Name    string
}

// MigrationInventory 返回规范迁移清单的副本，调用方只能读取。
func MigrationInventory() []Migration {
	inventory := make([]Migration, 0, len(sqliteMigrations))
	for _, migration := range sqliteMigrations {
		inventory = append(inventory, Migration{Version: migration.version, Name: migration.name})
	}
	return inventory
}

// PendingMigrations 返回来源版本之后仍未应用的迁移；来源为 0 时返回全部。
func PendingMigrations(source int) []Migration {
	inventory := MigrationInventory()
	pending := make([]Migration, 0, len(inventory))
	for _, migration := range inventory {
		if migration.Version > source {
			pending = append(pending, migration)
		}
	}
	return pending
}

// MigrationObserver 在启动迁移期间接收有界阶段通知。
// 实现必须是惰性的：它既不阻塞迁移，也不把原始错误文本写入产物。
type MigrationObserver interface {
	MigrationStarted(source int)
	MigrationApplied(migration Migration, current int)
	MigrationFailed(code string, rollbackRequired bool)
	MigrationCompleted(current int)
}

type migrationFailure struct {
	code string
	err  error
}

func (failure migrationFailure) Error() string { return failure.err.Error() }

func (failure migrationFailure) Unwrap() error { return failure.err }

func migrationFail(code string, err error) error {
	return migrationFailure{code: code, err: err}
}

// MigrationFailureCode 把迁移错误归类为固定代码；未包装的错误按类型分类。
func MigrationFailureCode(err error) string {
	var failure migrationFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	switch {
	case err == nil:
		return ""
	case errors.Is(err, os.ErrPermission),
		strings.Contains(err.Error(), "permission"),
		strings.Contains(err.Error(), "read-only"):
		return MigrationFailurePermission
	case isSQLiteLockError(err):
		return MigrationFailureBusy
	default:
		return MigrationFailureUnknown
	}
}

// notifyFailure 只在未来 schema 需要外部回滚时置位 rollback_required：
// 应用失败发生在事务内，回滚的是该事务，旧版本二进制仍然匹配来源 schema。
func notifyFailure(observer MigrationObserver, err error) {
	if observer == nil || err == nil {
		return
	}
	code := MigrationFailureCode(err)
	observer.MigrationFailed(code, code == MigrationFailureFutureSchema)
}
