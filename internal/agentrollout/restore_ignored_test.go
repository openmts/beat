package agentrollout

import "testing"

func TestRestoreRejectsIgnoredOrOverwrittenReconciliation(t *testing.T) {
	tests := []struct {
		name, trigger string
	}{
		{"rollout", "CREATE TRIGGER sabotage BEFORE UPDATE ON agent_rollouts BEGIN SELECT RAISE(IGNORE); END"},
		{"target", "CREATE TRIGGER sabotage BEFORE UPDATE ON agent_rollout_targets BEGIN SELECT RAISE(IGNORE); END"},
		{"attempt", "CREATE TRIGGER sabotage BEFORE UPDATE ON agent_rollout_attempts BEGIN SELECT RAISE(IGNORE); END"},
		{"release", "CREATE TRIGGER sabotage BEFORE UPDATE ON agent_releases BEGIN SELECT RAISE(IGNORE); END"},
		{"artifact", "CREATE TRIGGER sabotage BEFORE UPDATE ON agent_release_artifacts BEGIN SELECT RAISE(IGNORE); END"},
		{"after audit", `CREATE TRIGGER sabotage AFTER INSERT ON admin_audit_events WHEN NEW.action='rollout.restore.paused'
 BEGIN UPDATE agent_releases SET verified=1; END`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _ := offeredRestoreFixture(t)
			path, database := restoreSnapshot(t, f)
			if _, err := database.Exec(test.trigger); err != nil {
				t.Fatal(err)
			}
			if err := ReconcileRestore(t.Context(), path, f.now); err == nil {
				t.Fatal("restore accepted ignored or overwritten security state")
			}
			check := restoreAssertions{t, database}
			check.text("SELECT state FROM agent_rollouts", "active")
			check.text("SELECT state FROM agent_release_artifacts", "ready")
			check.count("SELECT COUNT(*) FROM agent_releases WHERE verified=1", 1)
			check.count("SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.restore.paused'", 0)
		})
	}
}

func TestRestoreNonceWriteFailureRollsBackEarlierStateChanges(t *testing.T) {
	for _, table := range []string{"agent_rollout_targets", "agent_rollout_attempts"} {
		t.Run(table, func(t *testing.T) {
			f, _ := offeredRestoreFixture(t)
			path, database := restoreSnapshot(t, f)
			query := "CREATE TRIGGER reject_nonce BEFORE UPDATE OF nonce_hash ON " + table + " BEGIN SELECT RAISE(FAIL,'fixture failure'); END"
			if _, err := database.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := ReconcileRestore(t.Context(), path, f.now); err == nil {
				t.Fatal("restore accepted failure to invalidate nonce")
			}
			check := restoreAssertions{t, database}
			check.text("SELECT state FROM agent_rollouts", "active")
			check.text("SELECT state FROM agent_rollout_attempts", "offered")
			check.count("SELECT COUNT(*) FROM agent_rollout_targets WHERE length(nonce_hash)=32", 1)
			check.count("SELECT COUNT(*) FROM agent_rollout_attempts WHERE length(nonce_hash)=32", 1)
			check.count("SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.restore.paused'", 0)
		})
	}
}
