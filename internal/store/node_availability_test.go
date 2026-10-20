package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func nodeStatusOf(t *testing.T, store *NodeStore, nodeID string) string {
	t.Helper()
	var status string
	if err := store.db.QueryRowContext(t.Context(), "SELECT status FROM nodes WHERE id=?", nodeID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func seedNodeStatus(t *testing.T, store *NodeStore, name string, status string, lastSeen time.Time) string {
	t.Helper()
	node, err := store.UpsertNode(t.Context(), name, "192.0.2.10", 22)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), "UPDATE nodes SET status=?, last_seen=? WHERE id=?", status, lastSeen, node.ID); err != nil {
		t.Fatal(err)
	}
	return node.ID
}

func TestMarkNodeStaleOfflineOnlyTouchesItsOwnStaleNode(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewNodeStore(sqlite.DB)
	now := model.NowUTC()
	cutoff := now.Add(-time.Hour)
	stale := seedNodeStatus(t, store, "stale-node", model.NodeStatusOnline, now.Add(-2*time.Hour))
	fresh := seedNodeStatus(t, store, "fresh-node", model.NodeStatusOnline, now.Add(-time.Minute))
	offline := seedNodeStatus(t, store, "already-offline", model.NodeStatusOffline, now.Add(-2*time.Hour))

	if err := store.MarkNodeStaleOffline(t.Context(), stale, cutoff); err != nil {
		t.Fatalf("mark stale node offline: %v", err)
	}
	if status := nodeStatusOf(t, store, stale); status != model.NodeStatusOffline {
		t.Fatalf("stale node status = %q", status)
	}
	if status := nodeStatusOf(t, store, fresh); status != model.NodeStatusOnline {
		t.Fatalf("fresh node status = %q", status)
	}
	if status := nodeStatusOf(t, store, offline); status != model.NodeStatusOffline {
		t.Fatalf("offline node status = %q", status)
	}
}

func TestMarkStaleNodesOfflineReportsAffectedRows(t *testing.T) {
	sqlite := setupTestDB(t)
	store := NewNodeStore(sqlite.DB)
	now := model.NowUTC()
	seedNodeStatus(t, store, "stale-one", model.NodeStatusOnline, now.Add(-3*time.Hour))
	seedNodeStatus(t, store, "stale-two", model.NodeStatusOnline, now.Add(-2*time.Hour))
	freshOne := seedNodeStatus(t, store, "fresh-one", model.NodeStatusOnline, now.Add(-time.Minute))

	updated, err := store.MarkStaleNodesOffline(t.Context(), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("mark stale nodes offline: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated nodes = %d, want 2", updated)
	}
	if status := nodeStatusOf(t, store, freshOne); status != model.NodeStatusOnline {
		t.Fatalf("fresh node status = %q", status)
	}
}
