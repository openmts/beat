package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestMarkStaleNodesOfflineAndHeartbeatRecovery(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	now := model.NowUTC()
	stale, err := nodes.UpsertNode(t.Context(), "stale", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create stale node: %v", err)
	}
	fresh, err := nodes.UpsertNode(t.Context(), "fresh", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create fresh node: %v", err)
	}
	if _, err := sqliteStore.DB.ExecContext(t.Context(),
		"UPDATE nodes SET last_seen = ? WHERE id = ?", now.Add(-2*time.Minute), stale.ID,
	); err != nil {
		t.Fatalf("age stale node: %v", err)
	}
	if _, err := sqliteStore.DB.ExecContext(t.Context(),
		"UPDATE nodes SET last_seen = ? WHERE id = ?", now.Add(-30*time.Second), fresh.ID,
	); err != nil {
		t.Fatalf("age fresh node: %v", err)
	}

	updated, err := nodes.MarkStaleNodesOffline(t.Context(), now.Add(-90*time.Second))
	if err != nil {
		t.Fatalf("mark stale nodes: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated nodes = %d, want 1", updated)
	}
	assertNodeStatus(t, nodes, stale.ID, model.NodeStatusOffline)
	assertNodeStatus(t, nodes, fresh.ID, model.NodeStatusOnline)

	if _, err := nodes.UpdateNodeHeartbeat(t.Context(), stale.ID, NodeHeartbeat{
		Host: "127.0.0.1", Port: 22,
	}); err != nil {
		t.Fatalf("restore heartbeat: %v", err)
	}
	assertNodeStatus(t, nodes, stale.ID, model.NodeStatusOnline)
}

func TestMarkStaleNodesOfflineClosedDatabase(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if _, err := nodes.MarkStaleNodesOffline(t.Context(), model.NowUTC()); err == nil {
		t.Fatal("expected closed database error")
	}
}

func assertNodeStatus(t *testing.T, nodes *NodeStore, id, want string) {
	t.Helper()
	node, err := nodes.GetNode(t.Context(), id)
	if err != nil {
		t.Fatalf("get node %s: %v", id, err)
	}
	if node == nil || node.Status != want {
		t.Fatalf("node %s status = %#v, want %s", id, node, want)
	}
}
