package remoteoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetryFailuresDoNotDiscardPriorAttempt(t *testing.T) {
	for _, item := range []struct{ table, verb string }{{"operation_task_targets", "UPDATE"}, {"operation_tasks", "UPDATE"}, {"admin_audit_events", "INSERT"}} {
		t.Run(item.table, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "retry-rollback")
			attempt := f.lease(t)
			if err := f.service.Start(t.Context(), attempt); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Complete(t.Context(), Completion{Attempt: attempt, Result: Result{State: "failed"}}); err != nil {
				t.Fatal(err)
			}
			task, err := f.service.GetTask(t.Context(), f.actor, id)
			if err != nil {
				t.Fatal(err)
			}
			f.sql(t, "CREATE TRIGGER reject_retry BEFORE "+item.verb+" ON "+item.table+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			if err := f.service.RetryTarget(t.Context(), f.actor, RetryInput{TargetRequest: TargetRequest{TaskID: id, TargetID: task.Targets[0].ID}, Reason: "checked host"}); err == nil {
				t.Fatal("partial retry acknowledged")
			}
			var current string
			if err := f.database.DB.QueryRow("SELECT current_attempt FROM operation_task_targets WHERE task_id=?", id).Scan(&current); err != nil || current != attempt.ID {
				t.Fatal(current, err)
			}
		})
	}
}

func TestRetentionDeletionFailuresPreserveHistory(t *testing.T) {
	for _, table := range []string{"operation_attempts", "operation_task_targets", "operation_tasks", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "retain-rollback")
			attempt := f.lease(t)
			if err := f.service.Start(t.Context(), attempt); err != nil {
				t.Fatal(err)
			}
			if err := f.service.Complete(t.Context(), Completion{Attempt: attempt, Result: Result{State: "succeeded"}}); err != nil {
				t.Fatal(err)
			}
			now := f.service.now().Add(40 * 24 * time.Hour)
			f.service.now = func() time.Time { return now }
			verb := "DELETE"
			if table == "admin_audit_events" {
				verb = "INSERT"
			}
			f.sql(t, "CREATE TRIGGER reject_retention BEFORE "+verb+" ON "+table+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			if err := f.service.Recover(t.Context()); err == nil {
				t.Fatal("partial retention succeeded")
			}
			var count int
			if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_tasks WHERE id=?", id).Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestQueueExpiryFailuresRollBackTerminalState(t *testing.T) {
	for _, table := range []string{"operation_attempts", "operation_task_targets", "admin_audit_events", "notification_messages"} {
		t.Run(table, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "expire-rollback")
			f.lease(t)
			now := f.service.now().Add(QueueLifetime + time.Second)
			f.service.now = func() time.Time { return now }
			verb, condition := "UPDATE", "WHEN NEW.state='expired'"
			if table == "admin_audit_events" || table == "notification_messages" {
				verb, condition = "INSERT", ""
			}
			f.sql(t, "CREATE TRIGGER reject_expiry BEFORE "+verb+" ON "+table+" "+condition+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			if err := f.service.Recover(t.Context()); err == nil {
				t.Fatal("expiry failure acknowledged")
			}
			var state string
			if err := f.database.DB.QueryRow("SELECT state FROM operation_task_targets WHERE task_id=?", id).Scan(&state); err != nil || state == "expired" {
				t.Fatal(state, err)
			}
		})
	}
}

func TestRecoveryKeepsActiveScanButRejectsOversizedInventory(t *testing.T) {
	f, _ := fileFixture(t)
	createUploaded(t, f, []byte("x"))
	ctx, cancel := context.WithCancel(t.Context())
	_ = ctx
	release, err := f.service.trackScan("live-scan", cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	f.sql(t, "UPDATE operation_artifacts SET state='scanning',scan_id='live-scan',scan_deadline=?", f.service.now().Add(time.Minute).UnixMilli())
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := f.database.DB.QueryRow("SELECT state FROM operation_artifacts").Scan(&state); err != nil || state != "scanning" {
		t.Fatal(state, err)
	}
	f.sql(t, `WITH RECURSIVE ids(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM ids WHERE n<256)
 INSERT INTO operation_artifacts(id,storage_name,metadata_encrypted,byte_size,digest,scan_status,state,actor_id,expires_at,created_at,updated_at)
 SELECT 'extra-'||n,'storage-'||n,X'00',0,'','pending','uploading','owner',9999999999999,1,1 FROM ids`)
	if err := f.service.Recover(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("oversized inventory accepted", err)
	}
}

func TestSpoolClosedHandlesAndUnsafeFilesFail(t *testing.T) {
	spool, err := OpenSpool(SpoolConfig{Directory: filepath.Join(t.TempDir(), "spool")})
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := spool.write("closed", nil); err == nil {
		t.Fatal("closed write")
	}
	if _, err := spool.read("closed"); err == nil {
		t.Fatal("closed read")
	}
	if err := spool.remove("closed"); err == nil {
		t.Fatal("closed delete")
	}
	if err := spool.syncDirectory(); err == nil {
		t.Fatal("closed directory sync")
	}
	file := filepath.Join(t.TempDir(), "not-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSpool(SpoolConfig{Directory: file}); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func TestClosedDatabaseRejectsRecoveryAndReadAdmission(t *testing.T) {
	f := testFixture(t)
	if err := f.database.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.recoveryArtifacts(t.Context()); err == nil {
		t.Fatal("closed inventory read")
	}
	if _, err := f.service.expiredTaskIDs(t.Context()); err == nil {
		t.Fatal("closed expiry read")
	}
	if err := f.service.expireQueuedTasks(t.Context()); err == nil {
		t.Fatal("closed expiry mutation")
	}
	if _, err := f.service.Status(t.Context(), f.actor); err == nil {
		t.Fatal("closed status admission")
	}
	if err := f.service.Recover(t.Context()); err == nil {
		t.Fatal("closed recovery admitted")
	}
}

func TestReadRefusesUnsafeSpoolModesAndPagingBounds(t *testing.T) {
	f, directory := fileFixture(t)
	path := filepath.Join(directory, "unsafe-mode")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.spool.read("unsafe-mode"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unsafe spool mode accepted", err)
	}
	if _, err := f.service.ListTaskPage(t.Context(), f.actor, TaskPageQuery{Limit: 201}); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := f.service.ListTaskPage(t.Context(), f.actor, TaskPageQuery{Cursor: string(make([]byte, 257))}); err != ErrInvalid {
		t.Fatal(err)
	}
}
