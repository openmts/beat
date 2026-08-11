package store

import (
	"context"
	"testing"
	"time"

	"github.com/openmts/mts"
)

func TestMTSStoreClosedDatabase(t *testing.T) {
	s, err := NewMTSStore(t.TempDir() + "/mts")
	if err != nil {
		t.Fatalf("create mts store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close mts store: %v", err)
	}
	ctx := context.Background()
	now := time.Now()
	if err := s.WriteMetric(ctx, "node", "cpu", 1, now); err == nil {
		t.Fatal("expected write error")
	}
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("flush after close should be safe: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close should be safe: %v", err)
	}
	if _, err := s.QueryMetrics(ctx, []string{"cpu"}, now.Add(-time.Hour), now, "node"); err != nil {
		t.Fatalf("query should degrade to empty result, got %v", err)
	}
	if err := s.ImportPoints(ctx, []mts.Point{{Measurement: "cpu"}}); err == nil {
		t.Fatal("import after close should error")
	}
}

func TestMTSStoreRejectsInvalidMetric(t *testing.T) {
	s, err := NewMTSStore(t.TempDir() + "/mts")
	if err != nil {
		t.Fatalf("create mts store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now()
	if _, err := s.QueryMetrics(context.Background(), []string{""}, now.Add(-time.Hour), now, "node"); err == nil {
		t.Fatal("expected invalid metric error")
	}
}

func TestDeleteGroupWithoutDefault(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(s.DB)
	group, err := groupStore.CreateGroup(ctx, "group")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM groups WHERE is_default = 1"); err != nil {
		t.Fatalf("delete default group: %v", err)
	}
	if err := groupStore.DeleteGroup(ctx, group.ID); err == nil {
		t.Fatal("expected missing default group error")
	}
}

func TestDeleteGroupNotFound(t *testing.T) {
	s := setupTestDB(t)
	if err := NewGroupStore(s.DB).DeleteGroup(context.Background(), "missing"); err == nil {
		t.Fatal("expected missing group error")
	}
}

func TestUpdateNodeNotFound(t *testing.T) {
	s := setupTestDB(t)
	node, err := NewNodeStore(s.DB).UpdateNode(context.Background(), "missing", NodeUpdate{})
	if err != nil {
		t.Fatalf("update missing node: %v", err)
	}
	if node != nil {
		t.Fatalf("node = %#v, want nil", node)
	}
}

func TestUpsertNodeWithoutDefaultGroup(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM groups WHERE is_default = 1"); err != nil {
		t.Fatalf("delete default group: %v", err)
	}
	if _, err := NewNodeStore(s.DB).UpsertNode(ctx, "node", "host", 22); err == nil {
		t.Fatal("expected missing default group error")
	}
}

func TestUpdateNodeDatabaseRejectsWrite(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "node", "host", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	_, err = s.DB.ExecContext(ctx, `
		CREATE TRIGGER reject_node_update BEFORE UPDATE ON nodes
		BEGIN SELECT RAISE(FAIL, 'update rejected'); END;
	`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := nodeStore.UpdateNode(ctx, node.ID, NodeUpdate{}); err == nil {
		t.Fatal("expected update rejection")
	}
}

func TestUpsertNodeDatabaseRejectsHeartbeat(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	if _, err := nodeStore.UpsertNode(ctx, "node", "host", 22); err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	_, err := s.DB.ExecContext(ctx, `
		CREATE TRIGGER reject_heartbeat BEFORE UPDATE ON nodes
		BEGIN SELECT RAISE(FAIL, 'heartbeat rejected'); END;
	`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := nodeStore.UpsertNode(ctx, "node", "new-host", 2222); err == nil {
		t.Fatal("expected heartbeat rejection")
	}
}

func TestUpdateNodeHeartbeatRejectsWrite(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	node, err := nodeStore.UpsertNode(ctx, "node", "host", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	_, err = s.DB.ExecContext(ctx, `
		CREATE TRIGGER reject_node_heartbeat BEFORE UPDATE ON nodes
		BEGIN SELECT RAISE(FAIL, 'node heartbeat rejected'); END;
	`)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if _, err := nodeStore.UpdateNodeHeartbeat(ctx, node.ID, NodeHeartbeat{Name: "node", Host: "new-host", Port: 2222}); err == nil {
		t.Fatal("expected node heartbeat rejection")
	}
}

func TestSQLiteStoreInvalidPath(t *testing.T) {
	if _, err := NewSQLiteStore("/dev/null/beat.db"); err == nil {
		t.Fatal("expected invalid path error")
	}
}
