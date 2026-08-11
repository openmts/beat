package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestNewSQLiteStoreMigratesLegacyNodeColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE nodes (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, alias TEXT, group_id TEXT,
		host TEXT NOT NULL, port INTEGER NOT NULL, status TEXT, ssh_public_key TEXT,
		last_seen DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy nodes table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO nodes
		(id, name, alias, group_id, host, port, status, ssh_public_key, created_at, updated_at)
		VALUES ('legacy', 'legacy-node', '', NULL, '127.0.0.1', 22, 'offline', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("insert legacy node: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	for range 2 {
		store, err := NewSQLiteStore(path)
		if err != nil {
			t.Fatalf("open migrated database: %v", err)
		}
		for name := range nodeColumns {
			var count int
			query := "SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name = ?"
			if err := store.DB.QueryRow(query, name).Scan(&count); err != nil {
				t.Fatalf("query migrated column %s: %v", name, err)
			}
			if count != 1 {
				t.Fatalf("column %s count = %d, want 1", name, count)
			}
		}
		var isPublic bool
		var tags string
		if err := store.DB.QueryRow(
			"SELECT is_public, tags FROM nodes WHERE id = 'legacy'",
		).Scan(&isPublic, &tags); err != nil {
			t.Fatalf("query migrated node presentation: %v", err)
		}
		if !isPublic || tags != "[]" {
			t.Fatalf("migrated node presentation = public:%v tags:%q", isPublic, tags)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("close migrated database: %v", err)
		}
	}
}
