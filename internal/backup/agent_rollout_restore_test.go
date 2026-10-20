package backup

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAgentRolloutArchiveRestorePausesAndInvalidatesBeforeActivation(t *testing.T) {
	fixture := rolloutRestoreFixture(t)
	target := stageAuthSnapshot(t, fixture)
	path := filepath.Join(target, "beat.db")
	if err := ApplyPending(t.Context(), target, path, filepath.Join(target, "beat_mts")); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	check := rolloutRestoreCheck{t, database}
	check.count("SELECT COUNT(*) FROM agent_rollouts WHERE state='paused' AND reason='restored'", 1)
	check.count("SELECT COUNT(*) FROM agent_rollout_targets WHERE state='held' AND nonce_hash IS NULL AND lease_expires_at IS NULL AND healthy_since IS NULL", 1)
	check.count("SELECT COUNT(*) FROM agent_rollout_attempts WHERE state='unknown' AND length(nonce_hash)=0", 1)
	check.count("SELECT COUNT(*) FROM agent_releases WHERE verified=0", 1)
	check.count("SELECT COUNT(*) FROM agent_release_artifacts WHERE state='missing'", 1)
	if err := ApplyPending(t.Context(), target, path, filepath.Join(target, "beat_mts")); err != nil {
		t.Fatal(err)
	}
	check.count("SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.restore.paused'", 1)
	live := rolloutRestoreCheck{t, fixture.sqlite.DB}
	live.count("SELECT COUNT(*) FROM agent_rollouts WHERE state='active'", 1)
	live.count("SELECT COUNT(*) FROM agent_rollout_targets WHERE state='offered' AND length(nonce_hash)=32", 1)
	live.count("SELECT COUNT(*) FROM agent_releases WHERE verified=1", 1)
}

func TestAgentRolloutRestoreAuditFailurePreservesLiveFiles(t *testing.T) {
	fixture := rolloutRestoreFixture(t)
	if _, err := fixture.sqlite.DB.Exec(`CREATE TRIGGER refuse_rollout_restore BEFORE INSERT ON admin_audit_events
 WHEN NEW.action='rollout.restore.paused' BEGIN SELECT RAISE(FAIL,'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	target := stageAuthSnapshot(t, fixture)
	path := filepath.Join(target, "beat.db")
	metrics := filepath.Join(target, "beat_mts")
	if err := os.Mkdir(metrics, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, "prior database")
	writeTestFile(t, filepath.Join(metrics, "marker"), "prior metrics")
	writeTestFile(t, filepath.Join(target, "admin-data.key"), "prior key")
	if err := ApplyPending(t.Context(), target, path, metrics); err == nil {
		t.Fatal("failed rollout reconciliation activated the archive")
	}
	assertFileContents(t, path, []byte("prior database"))
	assertFileContents(t, filepath.Join(metrics, "marker"), []byte("prior metrics"))
	assertFileContents(t, filepath.Join(target, "admin-data.key"), []byte("prior key"))
	leftovers, err := filepath.Glob(filepath.Join(target, "*.restore-new-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("failed restore left staged state", leftovers, err)
	}
}

func rolloutRestoreFixture(t *testing.T) *backupFixture {
	t.Helper()
	fixture := newBackupFixture(t)
	artifact := seedArchivedAgentRelease(t, fixture.sqlite.DB)
	seedRolloutRestoreState(t, fixture, artifact)
	return fixture
}

func seedRolloutRestoreState(t *testing.T, fixture *backupFixture, artifact model.BackupAgentReleaseArtifact) {
	t.Helper()
	now := time.Now().UTC()
	nonce := bytes.Repeat([]byte{3}, 32)
	statements := []struct {
		query string
		args  []any
	}{
		{"UPDATE agent_releases SET verified=1", nil},
		{"UPDATE agent_release_artifacts SET state='ready'", nil},
		{`INSERT INTO agent_rollouts(id,release_id,name,state,reason,policy_json,current_wave,total_waves,created_by,created_at,updated_at)
 VALUES('restore-campaign',?,'Restore fixture','active','','{"canary_count":1,"wave_size":1,"observation_seconds":30}',0,1,'fixture',?,?)`, []any{artifact.ReleaseID, now, now}},
		{`INSERT INTO agent_rollout_targets(id,rollout_id,node_snapshot_id,node_name,source_version,os,arch,wave,state,reason,current_attempt_id,artifact_id,nonce_hash,lease_expires_at,healthy_since,updated_at)
 VALUES('restore-target','restore-campaign','fixture-node','Fixture node','1.0.0','linux','amd64',0,'offered','','restore-attempt',?,?,?,?,?)`, []any{artifact.ArtifactID, nonce, now.Add(time.Hour), now, now}},
		{`INSERT INTO agent_rollout_attempts(id,target_id,nonce_hash,state,created_at)
 VALUES('restore-attempt','restore-target',?,'offered',?)`, []any{nonce, now}},
	}
	for _, statement := range statements {
		if _, err := fixture.sqlite.DB.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}

type rolloutRestoreCheck struct {
	t        *testing.T
	database *sql.DB
}

func (check rolloutRestoreCheck) count(query string, expected int) {
	check.t.Helper()
	var count int
	if err := check.database.QueryRow(query).Scan(&count); err != nil || count != expected {
		check.t.Fatalf("want %d, got %d (%v)", expected, count, err)
	}
}
