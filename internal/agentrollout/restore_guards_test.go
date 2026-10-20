package agentrollout

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestRestoreKeepsTerminalTargetsAndInvalidatesHistoricalNonces(t *testing.T) {
	for _, state := range []string{"completed", "aborted"} {
		t.Run(state, func(t *testing.T) {
			f, id := offeredRestoreFixture(t)
			if _, err := f.db.DB.Exec("UPDATE agent_rollouts SET state=?,reason='restored' WHERE id=?", state, id); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.DB.Exec("UPDATE agent_rollout_targets SET state='healthy'"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.DB.Exec("UPDATE agent_rollout_attempts SET state='healthy',ended_at=?", f.now); err != nil {
				t.Fatal(err)
			}
			path, database := restoreSnapshot(t, f)
			if err := ReconcileRestore(t.Context(), path, f.now); err != nil {
				t.Fatal(err)
			}
			(restoreAssertions{t, database}).text("SELECT state FROM agent_rollouts", state)
			(restoreAssertions{t, database}).text("SELECT state FROM agent_rollout_targets", "healthy")
			(restoreAssertions{t, database}).text("SELECT state FROM agent_rollout_attempts", "healthy")
			(restoreAssertions{t, database}).count("SELECT COUNT(*) FROM agent_rollout_targets WHERE nonce_hash IS NOT NULL OR lease_expires_at IS NOT NULL", 0)
			(restoreAssertions{t, database}).count("SELECT COUNT(*) FROM agent_rollout_attempts WHERE length(nonce_hash)>0", 0)
		})
	}
}

func TestRestoreAuditFailureRollsBackAllRolloutChanges(t *testing.T) {
	f, _ := offeredRestoreFixture(t)
	path, database := restoreSnapshot(t, f)
	if _, err := database.Exec(`CREATE TRIGGER fail_restore_audit BEFORE INSERT ON admin_audit_events
 WHEN NEW.action='rollout.restore.paused' BEGIN SELECT RAISE(FAIL,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), path, f.now); err == nil {
		t.Fatal("restore accepted a failed audit")
	}
	(restoreAssertions{t, database}).text("SELECT state FROM agent_rollouts", "active")
	(restoreAssertions{t, database}).text("SELECT state FROM agent_rollout_targets", "offered")
	(restoreAssertions{t, database}).text("SELECT state FROM agent_rollout_attempts", "offered")
	(restoreAssertions{t, database}).count("SELECT COUNT(*) FROM agent_releases WHERE verified=1", 1)
	(restoreAssertions{t, database}).count("SELECT COUNT(*) FROM agent_release_artifacts WHERE state='ready'", 1)
	(restoreAssertions{t, database}).count("SELECT COUNT(*) FROM agent_rollout_targets WHERE length(nonce_hash)=32", 1)
	(restoreAssertions{t, database}).count("SELECT COUNT(*) FROM agent_rollout_attempts WHERE length(nonce_hash)=32", 1)
}

func TestRestoreRejectsPartialOrClaimedMissingSchema(t *testing.T) {
	cases := []struct {
		name, schema string
		wantError    bool
	}{
		{"empty legacy", "", false},
		{"old ledger", "CREATE TABLE schema_migrations(version INTEGER); INSERT INTO schema_migrations VALUES(14)", false},
		{"missing v15 tables", "CREATE TABLE schema_migrations(version INTEGER); INSERT INTO schema_migrations VALUES(15)", true},
		{"partial tables", "CREATE TABLE agent_rollouts(id TEXT)", true},
		{"view impostor", "CREATE VIEW agent_rollouts AS SELECT 1 AS id", true},
		{"uppercase module", "CREATE TABLE AGENT_ROLLOUTS(id TEXT)", true},
		{"uppercase ledger", "CREATE TABLE SCHEMA_MIGRATIONS(version INTEGER); INSERT INTO SCHEMA_MIGRATIONS VALUES(14)", true},
		{"ledger view", "CREATE VIEW schema_migrations AS SELECT 14 AS version", true},
		{"broken ledger", "CREATE TABLE schema_migrations(other INTEGER)", true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			if test.schema != "" {
				if _, err := database.Exec(test.schema); err != nil {
					t.Fatal(err)
				}
			}
			present, err := restoredSchemaPresent(t.Context(), database)
			if present || (err != nil) != test.wantError {
				t.Fatalf("want error=%v, got present=%v error=%v", test.wantError, present, err)
			}
		})
	}
}

func TestRestoreSchemaChecksRespectCancellationAndClosedDatabase(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := restoredSchemaPresent(ctx, database); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled schema query did not stop", err)
	}
	if err := validateLegacyRestoreSchema(ctx, database); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled ledger query did not stop", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := restoredSchemaPresent(t.Context(), database); err == nil {
		t.Fatal("closed database accepted")
	}
}

func offeredRestoreFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal("missing fixture assignment", err)
	}
	return f, rollout.ID
}

func restoreSnapshot(t *testing.T, f *fixture) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "restore.db")
	if err := f.db.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return path, database
}

type restoreAssertions struct {
	t        *testing.T
	database *sql.DB
}

func (assert restoreAssertions) text(query, expected string) {
	assert.t.Helper()
	var value string
	if err := assert.database.QueryRow(query).Scan(&value); err != nil || value != expected {
		assert.t.Fatalf("want %q, got %q (%v)", expected, value, err)
	}
}

func (assert restoreAssertions) count(query string, expected int) {
	assert.t.Helper()
	var value int
	if err := assert.database.QueryRow(query).Scan(&value); err != nil || value != expected {
		assert.t.Fatalf("want %d, got %d (%v)", expected, value, err)
	}
}
