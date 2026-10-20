package remoteoperations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type operationsSnapshot struct{ directory, path string }

func snapshotOperations(t *testing.T, f fixture) operationsSnapshot {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "restored.db")
	if err := f.database.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(f.secretsDirectory, "admin-data.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	if err := os.WriteFile(filepath.Join(directory, "admin-data.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	return operationsSnapshot{directory: directory, path: path}
}

func loadRestoredOperations(t *testing.T, f fixture, snapshot operationsSnapshot) *Service {
	t.Helper()
	if err := ReconcileRestore(t.Context(), snapshot.path, f.service.now()); err != nil {
		t.Fatal(err)
	}
	db, err := store.NewSQLiteStore(snapshot.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	secrets := testutil.OpenSecrets(t, db.DB, snapshot.directory)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: db.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{Database: db.DB, Secrets: secrets, Queue: queue, Enabled: true, Now: f.service.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service
}

func TestRestoreNeverReplaysWorkStartedAfterSnapshot(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "snapshot-queued")
	snapshot := snapshotOperations(t, f)
	original := f.lease(t)
	if err := f.service.Start(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	restored := loadRestoredOperations(t, f, snapshot)
	lease, err := restored.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest})
	if err != nil || lease != nil {
		t.Fatal("restored queued work was automatically reissued", err)
	}
	task, err := restored.GetTask(t.Context(), f.actor, id)
	if err != nil || task.State != "unknown" || !task.RestoreRisk || !task.Targets[0].RestoreRisk {
		t.Fatal(task, err)
	}
	if err := restored.Start(t.Context(), original); err != ErrNotFound {
		t.Fatal("post-snapshot attempt unexpectedly known", err)
	}
	input := RetryInput{TargetRequest: TargetRequest{TaskID: id, TargetID: task.Targets[0].ID}, Reason: "operator checked the host"}
	if err := restored.RetryTarget(t.Context(), f.actor, input); err != ErrConflict {
		t.Fatal(err)
	}
	input.AcknowledgeUnknownRisk = true
	if err := restored.RetryTarget(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	live, err := f.service.GetTask(t.Context(), f.actor, id)
	if err != nil || live.State != "running" || live.RestoreRisk {
		t.Fatal("restore modified original database", live, err)
	}
}

func TestRestoreLeasedSnapshotRejectsStartButRecordsAuthenticatedLateResult(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "snapshot-leased")
	attempt := f.lease(t)
	snapshot := snapshotOperations(t, f)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	restored := loadRestoredOperations(t, f, snapshot)
	if err := restored.Start(t.Context(), attempt); err != ErrConflict {
		t.Fatal("restored lease authorized execution", err)
	}
	if cancel, err := restored.Heartbeat(t.Context(), attempt); err != nil || !cancel {
		t.Fatal("restored lease did not request stop", cancel, err)
	}
	completion := Completion{Attempt: attempt, Result: Result{State: "interrupted", Stdout: []byte("late private result")}}
	if err := restored.Complete(t.Context(), completion); err != nil {
		t.Fatal(err)
	}
	if err := restored.Complete(t.Context(), completion); err != nil {
		t.Fatal("late result retry", err)
	}
	task, err := restored.GetTask(t.Context(), f.actor, id)
	if err != nil || task.Targets[0].StartedAt != nil || task.Targets[0].State != "interrupted" {
		t.Fatal("invented restored start authorization", task, err)
	}
}

func TestRestoredTerminalTargetStillRequiresRetryRiskAcknowledgement(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "snapshot-failed")
	attempt := f.lease(t)
	if err := f.service.Start(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: attempt, Result: Result{State: "failed"}}); err != nil {
		t.Fatal(err)
	}
	restored := loadRestoredOperations(t, f, snapshotOperations(t, f))
	task, err := restored.GetTask(t.Context(), f.actor, id)
	if err != nil {
		t.Fatal(err)
	}
	input := RetryInput{TargetRequest: TargetRequest{TaskID: id, TargetID: task.Targets[0].ID}, Reason: "reviewed the restored host"}
	if err := restored.RetryTarget(t.Context(), f.actor, input); err != ErrConflict {
		t.Fatal(err)
	}
	input.AcknowledgeUnknownRisk = true
	if err := restored.RetryTarget(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
}

func TestDeletedSnippetCannotBeReadButTaskPayloadRemains(t *testing.T) {
	f := testFixture(t)
	snippet, err := f.service.SaveSnippet(t.Context(), f.actor, SnippetInput{Name: "Shared", Visibility: "shared", ShellFamily: "sh", Content: []byte("immutable content")})
	if err != nil {
		t.Fatal(err)
	}
	input := f.input("copied-content")
	input.Command = []byte("immutable content")
	id, err := f.service.CreateTask(t.Context(), f.actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteSnippet(t.Context(), f.actor, snippet.ID); err != nil {
		t.Fatal(err)
	}
	if content, err := f.service.SnippetContent(t.Context(), f.actor, snippet); err != ErrNotFound || len(content) != 0 {
		t.Fatal("deleted snippet remained readable", err)
	}
	content, err := f.service.TaskContent(t.Context(), f.actor, id)
	if err != nil || string(content.Payload.Command) != "immutable content" {
		t.Fatal("task history was deleted with snippet", err)
	}
}
