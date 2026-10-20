package store

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"
)

const agentDiscoveryMigration = 26

// TestAgentDiscoveryMigrationBackfillsCredentialState 验证 v25 -> v26 升级按 token
// 状态确定性回填 credential_state，且不触碰既有节点身份与新增表的初始状态。
func TestAgentDiscoveryMigrationBackfillsCredentialState(t *testing.T) {
	database := preAgentDiscoveryFixture(t)
	seedAgentDiscoveryNodes(t, database.DB)
	for range 2 {
		if err := applyMigrations(t.Context(), database.DB); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
	}
	want := map[string]string{"active-node": "active", "legacy-node": "legacy", "revoked-node": "revoked"}
	for name, state := range want {
		var got string
		if err := database.DB.QueryRow("SELECT credential_state FROM nodes WHERE name = ?", name).Scan(&got); err != nil || got != state {
			t.Fatalf("node %s credential_state = %q (%v)", name, got, err)
		}
	}
	var version, entries, grants, challenges, requests int
	if err := database.DB.QueryRow("SELECT MAX(version), COUNT(*) FROM schema_migrations").Scan(&version, &entries); err != nil ||
		version != currentSchemaVersion || entries != currentSchemaVersion {
		t.Fatalf("migration ledger version %d entries %d (%v)", version, entries, err)
	}
	query := "SELECT (SELECT COUNT(*) FROM agent_enrollment_grants), (SELECT COUNT(*) FROM agent_enrollment_challenges), (SELECT COUNT(*) FROM agent_enrollment_requests)"
	if err := database.DB.QueryRow(query).Scan(&grants, &challenges, &requests); err != nil || grants != 0 || challenges != 0 || requests != 0 {
		t.Fatalf("migration seeded enrollment data: %d/%d/%d (%v)", grants, challenges, requests, err)
	}
	if err := ValidateSnapshotSchema(t.Context(), database.DB); err != nil {
		t.Fatal(err)
	}
}

func seedAgentDiscoveryNodes(t *testing.T, database *sql.DB) {
	t.Helper()
	seeds := []struct {
		id, name  string
		tokenHash any
		revokedAt any
	}{
		{"11111111-1111-1111-1111-111111111111", "active-node", bytes.Repeat([]byte{1}, 32), nil},
		{"22222222-2222-2222-2222-222222222222", "legacy-node", nil, nil},
		{"33333333-3333-3333-3333-333333333333", "revoked-node", bytes.Repeat([]byte{3}, 32), "2026-01-01T00:00:00Z"},
	}
	for _, seed := range seeds {
		if _, err := database.ExecContext(t.Context(),
			"INSERT INTO nodes (id,name,host,port,agent_token_hash,agent_token_revoked_at,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)",
			seed.id, seed.name, "192.0.2.10", 22, seed.tokenHash, seed.revokedAt,
			"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatalf("seed node %s: %v", seed.name, err)
		}
	}
}

func preAgentDiscoveryFixture(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pre-v26.db")
	if err := secureSQLiteFile(path); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", sqliteConnectionString(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at DATETIME NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, migration := range sqliteMigrations {
		if migration.version >= agentDiscoveryMigration {
			break
		}
		if err := applyMigration(t.Context(), database, migration); err != nil {
			t.Fatal(err)
		}
	}
	return &SQLiteStore{DB: database, path: path}
}
