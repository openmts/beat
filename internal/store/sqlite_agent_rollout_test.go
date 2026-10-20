package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

const agentRolloutMigration = 15

func TestAgentRolloutMigrationUpgradesV14WithoutChangingNodes(t *testing.T) {
	database := preRolloutMigrationFixture(t)
	// 预升级快照里还没有 v22 的生命周期列，因此这里用原始 SQL 播种历史节点。
	nodeID := "11111111-1111-1111-1111-111111111111"
	if _, err := database.DB.ExecContext(t.Context(),
		"INSERT INTO nodes (id,name,host,port,created_at,updated_at) VALUES (?,?,?,?,?,?)",
		nodeID, "existing-node", "192.0.2.2", 22, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := applyMigrations(t.Context(), database.DB); err != nil {
			t.Fatal(err)
		}
	}
	var version, entries, trust, rollouts int
	if err := database.DB.QueryRow("SELECT MAX(version),COUNT(*) FROM schema_migrations").Scan(&version, &entries); err != nil || version != currentSchemaVersion || entries != currentSchemaVersion {
		t.Fatal("migration ledger was not complete", version, entries, err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM agent_release_trust").Scan(&trust); err != nil {
		t.Fatal(err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM agent_rollouts").Scan(&rollouts); err != nil || trust != 0 || rollouts != 0 {
		t.Fatal("migration activated trust or rollouts", trust, rollouts, err)
	}
	var retained string
	if err := database.DB.QueryRow("SELECT id FROM nodes WHERE name='existing-node'").Scan(&retained); err != nil || retained != nodeID {
		t.Fatal("migration altered existing node identity", err)
	}
	if err := ValidateSnapshotSchema(t.Context(), database.DB); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRolloutMigrationFailureRollsBackAllNewTablesAndLedger(t *testing.T) {
	database := preRolloutMigrationFixture(t)
	if _, err := database.DB.Exec("CREATE TABLE agent_rollouts(existing TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(t.Context(), database.DB); err == nil {
		t.Fatal("conflicting v15 schema was accepted")
	}
	var version, added int
	if err := database.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != agentRolloutMigration-1 {
		t.Fatal("failed migration advanced ledger", version, err)
	}
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN('agent_release_signature_policies','agent_release_signature_state','agent_release_trust','agent_releases','agent_release_artifacts')").Scan(&added); err != nil || added != 0 {
		t.Fatal("failed migration left partial tables", added, err)
	}
}

func preRolloutMigrationFixture(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pre-v15.db")
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
		if migration.version >= agentRolloutMigration {
			break
		}
		if err := applyMigration(t.Context(), database, migration); err != nil {
			t.Fatal(err)
		}
	}
	return &SQLiteStore{DB: database, path: path}
}
