package remoteoperations

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestoreMutationFailuresLeavePrivateSnapshotUnchanged(t *testing.T) {
	for _, table := range []string{"operation_tasks", "operation_attempts", "operation_task_targets", "operation_artifacts", "admin_audit_events"} {
		t.Run(table, func(t *testing.T) {
			f := testFixture(t)
			id := f.task(t, "restore-failure")
			f.lease(t)
			snapshot := snapshotOperations(t, f)
			db, err := sql.Open("sqlite", snapshot.path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			verb := "UPDATE"
			if table == "admin_audit_events" {
				verb = "INSERT"
			}
			if _, err := db.Exec("CREATE TRIGGER reject_restore BEFORE " + verb + " ON " + table + " BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
				t.Fatal(err)
			}
			if table == "operation_artifacts" {
				// 空 artifact 表不会触发 UPDATE；改为验证缺失依赖的恢复失败。
				if _, err := db.Exec("DROP TRIGGER reject_restore"); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("ALTER TABLE operation_artifacts RENAME TO unavailable_artifacts"); err != nil {
					t.Fatal(err)
				}
			}
			if err := ReconcileRestore(t.Context(), snapshot.path, f.service.now()); err == nil {
				t.Fatal("restore mutation failure ignored")
			}
			var state string
			if err := db.QueryRow("SELECT state FROM operation_task_targets WHERE task_id=?", id).Scan(&state); err != nil || state != "leased" {
				t.Fatal("partial restore committed", state, err)
			}
		})
	}
}

func TestRestoreSkipsOlderSchemasAndRejectsMissingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "older.db")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE older_schema(id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), filepath.Join(t.TempDir(), "missing.db"), time.Now()); err == nil {
		t.Fatal("missing snapshot created")
	}
}

func TestForceDeletionStorageFailuresNeverDetachNode(t *testing.T) {
	for _, item := range []struct{ table, verb string }{{"operation_attempts", "UPDATE"}, {"operation_task_targets", "UPDATE"}, {"operation_transfer_targets", "UPDATE"}, {"nodes", "UPDATE"}, {"nodes", "DELETE"}, {"admin_audit_events", "INSERT"}, {"notification_messages", "INSERT"}} {
		t.Run(item.table+item.verb, func(t *testing.T) {
			f := transferFault(t, "collect_source")
			f.sql(t, "CREATE TRIGGER reject_force BEFORE "+item.verb+" ON "+item.table+" BEGIN SELECT RAISE(ABORT,'fixture'); END")
			if err := f.service.ForceDeleteNode(t.Context(), f.actor, ForceNodeDelete{NodeID: f.nodeID, Reason: "reviewed host", AcknowledgeUnknownRisk: true}); err == nil {
				t.Fatal("partial force deletion acknowledged")
			}
			var count int
			if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM nodes WHERE id=?", f.nodeID).Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestDownloadFailuresNeverSkipCurrentAuthorityOrIntegrity(t *testing.T) {
	for _, mode := range []string{"consumer", "revoke", "cancel", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			f := transferFault(t, "download")
			if mode == "metadata" {
				installCryptoFault(t, f.fixture, cryptoFaultSecrets{open: "operation_artifact:metadata"})
			}
			err := f.service.DownloadTransfer(t.Context(), f.actor, DownloadInput{ID: f.transfer.ID, Consume: func(_ DownloadInfo, reader io.Reader) error {
				if _, err := reader.Read(nil); err != nil {
					return err
				}
				switch mode {
				case "consumer":
					return errors.New("fixture disconnect")
				case "revoke":
					f.sql(t, "UPDATE admin_sessions SET revoked_at=?", f.service.now())
				case "cancel":
					if err := f.service.CancelTransfer(t.Context(), f.actor, f.transfer.ID); err != nil {
						return err
					}
				}
				_, err := io.Copy(io.Discard, reader)
				return err
			}})
			if err == nil {
				t.Fatal("invalid download completed")
			}
		})
	}
}

func TestCloseCancelsAndJoinsActiveScans(t *testing.T) {
	f, _ := fileFixture(t)
	scanner := waitingScanner{started: make(chan struct{})}
	f.service.spool.scanner = scanner
	transfer := createUploaded(t, f, []byte("source"))
	finished := make(chan error, 1)
	go func() { _, err := f.service.FinalizeTransfer(t.Context(), f.actor, transfer.ID); finished <- err }()
	select {
	case <-scanner.started:
	case <-time.After(time.Second):
		t.Fatal("scan not started")
	}
	closed := make(chan struct{})
	go func() { f.service.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not join scanner")
	}
	if err := <-finished; err == nil {
		t.Fatal("shutdown scan finalized")
	}
}

func TestScannerConfigurationBoundsAndCanceledReaders(t *testing.T) {
	for _, config := range []CommandScannerConfig{{Executable: "/bin/cat", Arguments: make([]string, 33)}, {Executable: "/bin/cat", Arguments: []string{"x\x00y"}},
		{Executable: "/bin/cat", Arguments: []string{strings.Repeat("x", 1025)}}, {Executable: "/bin/cat", Timeout: time.Millisecond}, {Executable: "/bin/cat", Timeout: 3 * time.Minute}} {
		if _, err := NewCommandScanner(config); err != ErrInvalid {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := scanReader{ctx: ctx}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	reader.ctx = t.Context()
	if count, err := reader.Read(nil); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
