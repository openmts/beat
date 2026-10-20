package agentrollout

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreFaultsNeverPartiallyPauseSnapshot(t *testing.T) {
	for _, table := range []string{"agent_rollouts", "agent_rollout_attempts", "agent_rollout_targets", "agent_releases", "agent_release_artifacts", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			if assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || assignment == nil {
				t.Fatal(err)
			}
			operation := "UPDATE"
			if table == "admin_audit_events" {
				operation = "INSERT"
			}
			f.exec(t, "CREATE TRIGGER fail_restore BEFORE "+operation+" ON "+table+" BEGIN SELECT RAISE(ABORT,'full');END")
			path := filepath.Join(t.TempDir(), "restore.db")
			if err := f.db.Snapshot(t.Context(), path); err != nil {
				t.Fatal(err)
			}
			if err := ReconcileRestore(t.Context(), path, f.now); err == nil {
				t.Fatal("partial restore accepted")
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			var state string
			readErr := db.QueryRow("SELECT state FROM agent_rollouts WHERE id=?", rollout.ID).Scan(&state)
			closeErr := db.Close()
			if readErr != nil || closeErr != nil || state != "active" {
				t.Fatal(state, readErr, closeErr)
			}
		})
	}
}

func TestOldSnapshotsAndMissingFilesStaySafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE unrelated(id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), filepath.Join(t.TempDir(), "missing"), time.Now()); err == nil {
		t.Fatal("missing snapshot accepted")
	}
}

func TestExpiredReleasePausesOnceWithDurableEvidence(t *testing.T) {
	f := newFixture(t)
	rollout, _ := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(25 * time.Hour)
	for range 2 {
		if err := f.s.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	current, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.Reason != "release_expired" {
		t.Fatal(current, err)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollout_events WHERE kind='rollout.paused.automatic'", 1)
}
