package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

var errStageCommit = errors.New("staged commit failed")

func actorForTest() Actor {
	return Actor{ID: "admin", Username: "admin", Type: model.AuditActorAdmin, RequestID: "request"}
}

func writePendingJournal(t *testing.T, fixture *backupFixture, record model.BackupRecord, generation, origin string) {
	t.Helper()
	path, err := fixture.service.archivePath(record.Filename)
	if err != nil {
		t.Fatalf("resolve archive: %v", err)
	}
	journal := model.RestoreJournal{BackupID: record.ID, ArchivePath: path, State: "pending",
		Generation: generation, Origin: origin, CreatedAt: time.Now().UTC()}
	if err := writeJournal(fixture.service.journalPath(), journal); err != nil {
		t.Fatalf("write pending journal: %v", err)
	}
}

func journalExists(t *testing.T, fixture *backupFixture) bool {
	t.Helper()
	_, err := os.Lstat(fixture.service.journalPath())
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatalf("inspect journal: %v", err)
	}
	return true
}

// TestReconcileRollsBackUnconfirmedStaging 覆盖崩溃点：只有 staged 行加同代次 journal 才能保留。
func TestReconcileRollsBackUnconfirmedStaging(t *testing.T) {
	cases := []struct {
		name       string
		state      string
		generation string
		journal    string
		remove     bool
		wantState  string
		wantKept   bool
	}{
		{name: "staging with matching journal", state: model.BackupStateStaging,
			generation: "gen-1", journal: "gen-1", wantState: model.BackupStateReady},
		{name: "staging without journal", state: model.BackupStateStaging,
			generation: "gen-1", wantState: model.BackupStateReady},
		{name: "staged with mismatched generation", state: model.BackupStateStaged,
			generation: "gen-1", journal: "gen-2", wantState: model.BackupStateReady},
		{name: "staged without journal", state: model.BackupStateStaged,
			generation: "gen-1", wantState: model.BackupStateReady},
		{name: "staged with vanished archive", state: model.BackupStateStaged,
			generation: "gen-1", journal: "gen-1", remove: true, wantState: model.BackupStateReady},
		{name: "staged confirmed", state: model.BackupStateStaged,
			generation: "gen-1", journal: "gen-1", wantState: model.BackupStateStaged, wantKept: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBackupFixture(t)
			record := readyFixtureBackup(t, fixture)
			updateBackupRow(t, fixture, record.ID, map[string]any{
				"state": test.state, "previous_state": model.BackupStateReady,
				"restore_generation": test.generation})
			if test.journal != "" {
				writePendingJournal(t, fixture, record, test.journal, "")
			}
			if test.remove {
				if err := os.Remove(filepath.Join(fixture.service.root, record.Filename)); err != nil {
					t.Fatalf("remove archive: %v", err)
				}
			}
			outcome, err := fixture.service.Reconcile(t.Context())
			if err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			loaded := loadBackupRow(t, fixture, record.ID)
			if loaded == nil || loaded.State != test.wantState {
				t.Fatalf("record = %#v, want state %s", loaded, test.wantState)
			}
			if test.wantKept {
				if outcome.RolledBack != 0 || !journalExists(t, fixture) {
					t.Fatalf("confirmed restore was not preserved: %#v", outcome)
				}
				return
			}
			if outcome.RolledBack != 1 {
				t.Fatalf("rollback outcome = %#v", outcome)
			}
			if journalExists(t, fixture) && test.journal != "" {
				t.Fatal("unconfirmed journal survived reconciliation")
			}
		})
	}
}

func TestReconcileKeepsOfflineJournalForOperatorRecovery(t *testing.T) {
	fixture := newBackupFixture(t)
	journal := model.RestoreJournal{BackupID: "offline", ArchivePath: fixture.service.root + "/offline.zip",
		State: "pending", Origin: model.RestoreOriginOffline, CreatedAt: time.Now().UTC()}
	if err := writeJournal(fixture.service.journalPath(), journal); err != nil {
		t.Fatalf("write offline journal: %v", err)
	}
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.JournalsRemoved != 0 || !journalExists(t, fixture) {
		t.Fatalf("offline journal outcome = %#v, %v", outcome, err)
	}
}

func TestReconcileDiscardsJournalWithoutMatchingRecord(t *testing.T) {
	fixture := newBackupFixture(t)
	journal := model.RestoreJournal{BackupID: "missing", ArchivePath: fixture.service.root + "/gone.zip",
		State: "pending", Generation: "gen-1", CreatedAt: time.Now().UTC()}
	if err := writeJournal(fixture.service.journalPath(), journal); err != nil {
		t.Fatalf("write orphan journal: %v", err)
	}
	outcome, err := fixture.service.Reconcile(t.Context())
	if err != nil || outcome.JournalsRemoved != 1 || journalExists(t, fixture) {
		t.Fatalf("orphan journal outcome = %#v, %v", outcome, err)
	}
	if !hasAuditAction(t, fixture, "backup.reconcile.journal_discarded") {
		t.Fatal("discarded journal was not audited")
	}
}

// TestStageRestoreCommitsGenerationAndRollsBackOnFailure 覆盖在线暂存的三阶段提交顺序。
func TestStageRestoreCommitsGenerationAndRollsBackOnFailure(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	staged, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: record.ID,
		Confirmation: RestoreConfirmation, Actor: actorForTest()})
	if err != nil || staged.State != model.BackupStateStaged || staged.RestoreGeneration == "" {
		t.Fatalf("staged record = %#v, %v", staged, err)
	}
	journal, err := readJournal(fixture.service.journalPath())
	if err != nil || journal.Generation != staged.RestoreGeneration ||
		journal.State != "pending" || journal.Origin != "" {
		t.Fatalf("staged journal = %#v, %v", journal, err)
	}
	// 已存在别的待应用 journal 时，第二个暂存必须被拒绝而不是覆盖它。
	second := readyFixtureBackup(t, fixture)
	if _, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: second.ID,
		Confirmation: RestoreConfirmation, Actor: actorForTest()}); !errors.Is(err, ErrRestoreStaged) {
		t.Fatalf("conflicting staging = %v", err)
	}
	retained, err := readJournal(fixture.service.journalPath())
	if err != nil || retained.BackupID != record.ID {
		t.Fatalf("conflicting staging replaced the journal: %#v, %v", retained, err)
	}
}

// TestStageRestoreRollsBackWhenStagedCommitFails 覆盖“journal 已写但 staged 提交失败”的崩溃点。
func TestStageRestoreRollsBackWhenStagedCommitFails(t *testing.T) {
	fixture := newBackupFixture(t)
	record := readyFixtureBackup(t, fixture)
	forced := &stageFailureStore{recordStore: fixture.service.records, fail: true}
	fixture.service.records = forced
	if _, err := fixture.service.StageRestore(t.Context(), StageRequest{BackupID: record.ID,
		Confirmation: RestoreConfirmation, Actor: actorForTest()}); err == nil {
		t.Fatal("forced staged commit failure succeeded")
	}
	rolled := loadBackupRow(t, fixture, record.ID)
	if rolled == nil || rolled.State != model.BackupStateReady || rolled.RestoreGeneration != "" {
		t.Fatalf("rollback record = %#v", rolled)
	}
	if journalExists(t, fixture) {
		t.Fatal("failed staging left a usable journal")
	}
}

type stageFailureStore struct {
	recordStore
	fail bool
}

func (records *stageFailureStore) Transition(ctx context.Context, transition store.BackupTransition) error {
	if records.fail && transition.Record.State == model.BackupStateStaged {
		return errStageCommit
	}
	return records.recordStore.Transition(ctx, transition)
}
