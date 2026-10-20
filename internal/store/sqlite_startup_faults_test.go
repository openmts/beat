package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covClosedHandle 返回一个已关闭的句柄：调用它的查询会立即报错，
// 用来验证启动与迁移路径在存储不可用时失败关闭。
func covClosedHandle(t *testing.T) *sql.DB {
	t.Helper()
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	return handle
}

func TestSQLitePathHardeningRejectsUnusableTargets(t *testing.T) {
	directory := t.TempDir()
	if err := secureSQLiteFile(directory); err == nil {
		t.Fatal("securing a directory must fail")
	}
	blocker := filepath.Join(directory, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := secureSQLiteFiles(filepath.Join(blocker, "database.db")); err == nil {
		t.Fatal("securing a path below a regular file must fail")
	}
	if err := secureSQLiteFile(":memory:"); err != nil {
		t.Fatalf("in-memory database needs no hardening: %v", err)
	}
	if err := secureSQLiteFiles(":memory:"); err != nil {
		t.Fatalf("in-memory database needs no hardening: %v", err)
	}
	if got := sqliteFilesystemPath("file:/var/lib/beat.db?mode=ro"); got != "/var/lib/beat.db" {
		t.Fatalf("filesystem path = %q", got)
	}
}

func TestSQLiteWALGuardRejectsUnsupportedMode(t *testing.T) {
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := enableSQLiteWAL(ctx, handle, "/var/lib/beat/beat.db"); err == nil || !strings.Contains(err.Error(), "journal mode") {
		t.Fatalf("in-memory database must not claim WAL: %v", err)
	}
	if err := enableSQLiteWAL(ctx, handle, ":memory:"); err != nil {
		t.Fatalf("in-memory database needs no WAL switch: %v", err)
	}
}

func TestSQLiteOpenFailsClosedOnUnusableTargets(t *testing.T) {
	if _, err := NewSQLiteStoreObserved(t.Context(), t.TempDir(), nil); err == nil {
		t.Fatal("a directory database path must fail to open")
	}
	future := filepath.Join(t.TempDir(), "future.db")
	handle, err := sql.Open("sqlite", sqliteConnectionString(future))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Exec("INSERT INTO schema_migrations(version,name,applied_at) VALUES(?,?,?)",
		currentSchemaVersion+1, "future", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(future, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStoreObserved(t.Context(), future, nil); err == nil ||
		!strings.Contains(err.Error(), "migrating sqlite database") {
		t.Fatalf("future schema must fail startup: %v", err)
	}
}

func TestSQLiteOpenFailsClosedWhenStartupCleanupBreaks(t *testing.T) {
	store := setupTestDB(t)
	path := store.path
	if _, err := store.DB.Exec("DROP TABLE agent_enrollment_challenges"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLiteStoreObserved(t.Context(), path, nil); err == nil ||
		!strings.Contains(err.Error(), "cleaning enrollment state") {
		t.Fatalf("broken startup cleanup must fail startup: %v", err)
	}
}

func TestSQLiteReadyReportsSchemaAndProbeFailures(t *testing.T) {
	if err := closedSQLiteStore(t).Ready(t.Context()); err == nil {
		t.Fatal("closed database must not report ready")
	}
	stale := setupTestDB(t)
	if _, err := stale.DB.Exec("DELETE FROM schema_migrations WHERE version=?", currentSchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := stale.Ready(t.Context()); err == nil {
		t.Fatal("schema behind the binary must not report ready")
	}
	unwritable := setupTestDB(t)
	unwritable.path = filepath.Join(t.TempDir(), "missing", "database.db")
	if err := unwritable.Ready(t.Context()); err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("missing data directory must fail the write probe: %v", err)
	}
	ready := setupTestDB(t)
	if err := ready.Ready(t.Context()); err != nil {
		t.Fatalf("fixture database must report ready: %v", err)
	}
}
