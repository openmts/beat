package notificationqueue

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreReconcilesExpiredLeasesAndPreservesTerminalRows(t *testing.T) {
	s, db := queueFixture(t)
	addQueueChannel(t, s, "channel")
	if _, err := s.Enqueue(t.Context(), enqueueRequest("event"), nil); err != nil {
		t.Fatal(err)
	}
	if lease, err := s.claim(t.Context(), "old-worker"); err != nil || lease == nil {
		t.Fatal(err)
	}
	var path string
	if err := db.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), path, s.now().Add(deliveryLeaseLifetime+time.Second)); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.SourceReceipt(t.Context(), "event")
	if err != nil || receipt.State != "pending" {
		t.Fatal("expired lease did not return to pending")
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='notification.restore.reconcile'").Scan(&count); err != nil || count != 1 {
		t.Fatal("restore reconciliation audit missing")
	}
	if err := ReconcileRestore(t.Context(), filepath.Join(t.TempDir(), "missing.db"), s.now()); err == nil {
		t.Fatal("missing snapshot silently created")
	}
}
