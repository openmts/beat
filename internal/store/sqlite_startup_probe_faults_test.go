package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

// TestSecureSQLiteFilesToleratesMissingSidecars 覆盖 -wal/-shm 尚未创建时的跳过分支。
func TestSecureSQLiteFilesToleratesMissingSidecars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.db")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := secureSQLiteFiles(path); err != nil {
		t.Fatalf("missing sidecar files = %v", err)
	}
}

// TestSQLiteStartupFailsClosedOnBrokenConnections 覆盖打开后探测失败与
// 无法切换 WAL 两条启动失败路径。
func TestSQLiteStartupFailsClosedOnBrokenConnections(t *testing.T) {
	t.Run("ping failure", func(t *testing.T) {
		dsn := "file:cov-unreachable?mode=memory&cache=shared&vfs=cov-missing-vfs"
		if _, err := NewSQLiteStoreObserved(t.Context(), dsn, nil); err == nil ||
			!strings.Contains(err.Error(), "pinging sqlite database") {
			t.Fatalf("unreachable database = %v", err)
		}
	})
	t.Run("wal failure", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "readonly.db")
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		dsn := "file:" + path + "?mode=ro"
		if _, err := NewSQLiteStoreObserved(t.Context(), dsn, nil); err == nil ||
			!strings.Contains(err.Error(), "WAL") {
			t.Fatalf("read-only database = %v", err)
		}
	})
}

// TestEnableSQLiteWALPropagatesNonLockFailures 覆盖非锁故障直接冒泡。
func TestEnableSQLiteWALPropagatesNonLockFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.db")
	err := enableSQLiteWAL(t.Context(), covClosedHandle(t), path)
	if err == nil || !strings.Contains(err.Error(), "enable SQLite WAL") {
		t.Fatalf("closed handle = %v", err)
	}
}

// TestEnableSQLiteWALRetriesLockedDatabase 覆盖写锁下的重试直到上下文超时。
func TestEnableSQLiteWALRetriesLockedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.db")
	holder, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	if _, err := holder.ExecContext(t.Context(), "CREATE TABLE cov_holder(x INTEGER)"); err != nil {
		t.Fatal(err)
	}
	tx, err := holder.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO cov_holder VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	blocked, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocked.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	if err := enableSQLiteWAL(ctx, blocked, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("locked database = %v", err)
	}
}

// TestSQLiteReadyReportsMissingMigrationLedger 覆盖迁移账本不可读时的就绪失败。
func TestSQLiteReadyReportsMissingMigrationLedger(t *testing.T) {
	sqlite := setupTestDB(t)
	if _, err := sqlite.DB.Exec("DROP TABLE schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Ready(t.Context()); err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Fatalf("missing migration ledger = %v", err)
	}
}

// TestProbeDataDirectorySkipsEphemeralPaths 覆盖内存库与空路径跳过写探针。
func TestProbeDataDirectorySkipsEphemeralPaths(t *testing.T) {
	sqlite := setupTestDB(t)
	for _, path := range []string{":memory:", ""} {
		sqlite.path = path
		if err := sqlite.probeDataDirectory(); err != nil {
			t.Fatalf("probe %q = %v", path, err)
		}
	}
}

// TestEnsureDefaultGroupFailsClosedOnRepairFaults 覆盖默认分组扫描、归一、
// 插入与节点修复四条失败路径。
func TestEnsureDefaultGroupFailsClosedOnRepairFaults(t *testing.T) {
	t.Run("scan failure", func(t *testing.T) {
		sqlite := setupTestDB(t)
		if _, err := sqlite.DB.Exec("ALTER TABLE groups RENAME TO cov_groups_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DB.Exec(
			"CREATE VIEW groups AS SELECT NULL AS id, 1 AS is_default, 0 AS sort_order"); err != nil {
			t.Fatal(err)
		}
		if err := ensureDefaultGroup(t.Context(), sqlite.DB); err == nil {
			t.Fatal("unscannable default group was accepted")
		}
	})
	t.Run("normalize failure", func(t *testing.T) {
		sqlite := setupTestDB(t)
		if _, err := sqlite.DB.Exec("ALTER TABLE groups RENAME TO cov_groups_backup"); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlite.DB.Exec(`CREATE VIEW groups AS
			SELECT id, 1 AS is_default, sort_order FROM cov_groups_backup
			UNION ALL SELECT 'cov-extra-default', 1, 0`); err != nil {
			t.Fatal(err)
		}
		if err := ensureDefaultGroup(t.Context(), sqlite.DB); err == nil {
			t.Fatal("blocked default group normalization was accepted")
		}
	})
	t.Run("insert failure", func(t *testing.T) {
		sqlite := setupTestDB(t)
		if _, err := sqlite.DB.Exec("DELETE FROM groups"); err != nil {
			t.Fatal(err)
		}
		installCustomMetricFault(t, sqlite.DB, `CREATE TRIGGER cov_groups_insert
			BEFORE INSERT ON groups BEGIN SELECT RAISE(ABORT,'insert blocked'); END`)
		if err := ensureDefaultGroup(t.Context(), sqlite.DB); err == nil {
			t.Fatal("blocked default group insert was accepted")
		}
	})
	t.Run("node repair failure", func(t *testing.T) {
		sqlite := setupTestDB(t)
		now := model.NowUTC()
		if _, err := sqlite.DB.Exec(`INSERT INTO nodes (id, name, group_id, host, port, created_at, updated_at)
			VALUES (?, ?, NULL, ?, ?, ?, ?)`, uuid.NewString(), "cov-orphan-node", "192.0.2.9", 22, now, now); err != nil {
			t.Fatal(err)
		}
		installCustomMetricFault(t, sqlite.DB, `CREATE TRIGGER cov_nodes_repair
			BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'repair blocked'); END`)
		if err := ensureDefaultGroup(t.Context(), sqlite.DB); err == nil {
			t.Fatal("blocked node group repair was accepted")
		}
	})
}
