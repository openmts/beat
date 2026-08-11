package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestSQLiteConnectionPolicy(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "policy.db"))
	if err != nil {
		t.Fatalf("open SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for range 4 {
		connection, err := store.DB.Conn(context.Background())
		if err != nil {
			t.Fatalf("open SQLite connection: %v", err)
		}
		var foreignKeys, busyTimeout int
		var journalMode string
		if err := connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			_ = connection.Close()
			t.Fatalf("query foreign_keys: %v", err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			_ = connection.Close()
			t.Fatalf("query busy_timeout: %v", err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			_ = connection.Close()
			t.Fatalf("query journal_mode: %v", err)
		}
		if err := connection.Close(); err != nil {
			t.Fatalf("close SQLite connection: %v", err)
		}
		if foreignKeys != 1 || busyTimeout != 5000 || journalMode != "wal" {
			t.Fatalf("SQLite policy = foreign_keys:%d busy_timeout:%d journal_mode:%s",
				foreignKeys, busyTimeout, journalMode)
		}
	}
}

func TestSQLiteMigrationLedger(t *testing.T) {
	store := setupTestDB(t)
	if err := store.Ready(context.Background()); err != nil {
		t.Fatalf("SQLite readiness: %v", err)
	}
	var version int
	var count int
	if err := store.DB.QueryRow("SELECT MAX(version), COUNT(*) FROM schema_migrations").Scan(&version, &count); err != nil {
		t.Fatalf("query schema migrations: %v", err)
	}
	if version != currentSchemaVersion || count != currentSchemaVersion {
		t.Fatalf("migration ledger = version:%d count:%d", version, count)
	}
}

func TestSQLiteReadinessFailsAfterClose(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("open SQLite store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close SQLite store: %v", err)
	}
	if err := store.Ready(context.Background()); err == nil {
		t.Fatal("closed SQLite store reported ready")
	}
}

func TestSQLiteRejectsFutureMigrationVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("create SQLite store: %v", err)
	}
	if _, err := store.DB.Exec(
		"INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, 'future', CURRENT_TIMESTAMP)",
		currentSchemaVersion+1,
	); err != nil {
		t.Fatalf("insert future migration: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close SQLite store: %v", err)
	}
	if _, err := NewSQLiteStore(path); err == nil {
		t.Fatal("future schema version was accepted")
	}
}

func TestConcurrentSQLiteMigrationStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent-migration.db")
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			store, err := NewSQLiteStore(path)
			if err == nil {
				err = store.Close()
			}
			errorsCh <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatalf("concurrent migration startup: %v", err)
		}
	}
}

func TestMigrationNormalizesDefaultGroupsAndRepairsNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-defaults.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE groups (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, sort_order INTEGER DEFAULT 0,
		is_default INTEGER DEFAULT 0, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	); CREATE TABLE nodes (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, alias TEXT, group_id TEXT,
		host TEXT NOT NULL, port INTEGER NOT NULL, status TEXT, ssh_public_key TEXT,
		last_seen DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO groups VALUES
		('default-a', 'A', 0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
		('default-b', 'B', 1, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
		INSERT INTO nodes (id, name, group_id, host, port, created_at, updated_at)
		VALUES ('node', 'node', NULL, '127.0.0.1', 22, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("insert legacy data: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var defaults int
	var groupID string
	if err := store.DB.QueryRow("SELECT COUNT(*) FROM groups WHERE is_default = 1").Scan(&defaults); err != nil {
		t.Fatalf("count default groups: %v", err)
	}
	if err := store.DB.QueryRow("SELECT group_id FROM nodes WHERE id = 'node'").Scan(&groupID); err != nil {
		t.Fatalf("query repaired node: %v", err)
	}
	if defaults != 1 || groupID != "default-a" {
		t.Fatalf("normalized defaults = %d, node group = %q", defaults, groupID)
	}
}

func TestNodeHeartbeatMissingDefaultIsAtomic(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	if _, err := store.DB.ExecContext(ctx, "DELETE FROM groups"); err != nil {
		t.Fatalf("delete groups: %v", err)
	}
	nodes := NewNodeStore(store.DB)
	if _, err := nodes.UpsertNode(ctx, "missing-default", "127.0.0.1", 22); err == nil {
		t.Fatal("heartbeat without a default group was accepted")
	}
	var count int
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes WHERE name = ?", "missing-default").Scan(&count); err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if count != 0 {
		t.Fatalf("partial node rows = %d", count)
	}
}

func TestConcurrentFirstHeartbeatIsIdempotent(t *testing.T) {
	store := setupTestDB(t)
	nodes := NewNodeStore(store.DB)
	const workers = 8
	start := make(chan struct{})
	errorsCh := make(chan error, workers)
	ids := make(chan string, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			node, err := nodes.UpsertNode(context.Background(), "concurrent", "127.0.0.1", 22)
			if err == nil {
				ids <- node.ID
			}
			errorsCh <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	close(ids)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent heartbeat: %v", err)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("heartbeat IDs differ: %q and %q", first, id)
		}
	}
	var count int
	if err := store.DB.QueryRow("SELECT COUNT(*) FROM nodes WHERE name = 'concurrent'").Scan(&count); err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if count != 1 {
		t.Fatalf("node count = %d", count)
	}
}

func TestGroupSortRejectsUnknownID(t *testing.T) {
	store := setupTestDB(t)
	groups := NewGroupStore(store.DB)
	if err := groups.UpdateSortOrder(context.Background(), []string{"missing"}); err == nil {
		t.Fatal("unknown group sort ID was accepted")
	}
}
