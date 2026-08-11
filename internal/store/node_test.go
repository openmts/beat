package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestListNodes(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := []struct {
		id, name, host string
	}{
		{"node-1", "server-a", "192.168.0.1"},
		{"node-2", "server-b", "192.168.0.2"},
	}
	for _, d := range data {
		_, err := store.DB.ExecContext(ctx,
			"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			d.id, d.name, "", "", d.host, 22, model.NodeStatusOnline, "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodes, err := nodeStore.ListNodes(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestListNodesEmpty(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	nodes, err := nodeStore.ListNodes(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestListNodesByGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"node-g1", "in-group", "", "group-A", "10.0.0.1", 22, "online", "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"node-g0", "nogroup", "", "", "10.0.0.2", 22, "online", "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodes, err := nodeStore.ListNodes(ctx, "group-A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 node in group, got %d", len(nodes))
	}
	if nodes[0].Name != "in-group" {
		t.Errorf("expected name %q, got %q", "in-group", nodes[0].Name)
	}

	nodes, err = nodeStore.ListNodes(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for unknown group, got %d", len(nodes))
	}
}

func TestGetNode(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"get-node-id", "get-node", "my-alias", "", "10.0.0.5", 8080, "online", "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	node, err := nodeStore.GetNode(ctx, "get-node-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node == nil {
		t.Fatal("expected node, got nil")
	}
	if node.Name != "get-node" {
		t.Errorf("expected name %q, got %q", "get-node", node.Name)
	}
	if node.Alias != "my-alias" {
		t.Errorf("expected alias %q, got %q", "my-alias", node.Alias)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	node, err := nodeStore.GetNode(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Error("expected nil for non-existent node")
	}
}

func TestUpdateNode(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"update-node-id", "update-node", "old-alias", "", "10.0.0.10", 22, "online", "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := nodeStore.UpdateNode(ctx, "update-node-id", NodeUpdate{
		Alias:        "new-alias",
		GroupID:      "group-X",
		SSHPublicKey: "ssh-ed25519 assigned-key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updated == nil {
		t.Fatal("expected updated node, got nil")
	}
	if updated.Alias != "new-alias" {
		t.Errorf("expected alias %q, got %q", "new-alias", updated.Alias)
	}
	if updated.GroupID != "group-X" {
		t.Errorf("expected group_id %q, got %q", "group-X", updated.GroupID)
	}
	if updated.SSHPublicKey != "ssh-ed25519 assigned-key" {
		t.Errorf("expected assigned SSH public key, got %q", updated.SSHPublicKey)
	}
}

func TestDeleteNode(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"delete-node-id", "delete-me", "", "", "10.0.0.20", 22, "offline", "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = nodeStore.DeleteNode(ctx, "delete-node-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	node, err := nodeStore.GetNode(ctx, "delete-node-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Error("expected nil after delete")
	}
}

func TestUpsertNodeNew(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	node, err := nodeStore.UpsertNode(ctx, "new-upsert-node", "10.0.0.30", 2222)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if node == nil {
		t.Fatal("expected node, got nil")
	}
	if node.Name != "new-upsert-node" {
		t.Errorf("expected name %q, got %q", "new-upsert-node", node.Name)
	}
	if node.Host != "10.0.0.30" {
		t.Errorf("expected host %q, got %q", "10.0.0.30", node.Host)
	}
	if node.Port != 2222 {
		t.Errorf("expected port %d, got %d", 2222, node.Port)
	}
	if node.Status != model.NodeStatusOnline {
		t.Errorf("expected status %q, got %q", model.NodeStatusOnline, node.Status)
	}
	if node.ID == "" {
		t.Error("expected non-empty node ID")
	}
}

func TestUpsertNodeStoresSystemInfo(t *testing.T) {
	store := setupTestDB(t)
	nodeStore := NewNodeStore(store.DB)
	heartbeat := NodeHeartbeat{
		Name: "system-node", Host: "10.0.0.60", Port: 22,
		System: model.SystemInfo{
			CPUModel: "Test CPU", OS: "linux", Platform: "ubuntu", OSVersion: "24.04",
			Kernel: "6.8.0", Arch: "x86_64", Virtualization: "kvm guest", AgentVersion: "1.2.3",
		},
	}
	node, err := nodeStore.UpsertNodeWithSystem(context.Background(), heartbeat)
	if err != nil {
		t.Fatalf("upsert node with system info: %v", err)
	}
	if node.CPUModel != "Test CPU" || node.OS != "linux" || node.Platform != "ubuntu" ||
		node.OSVersion != "24.04" || node.Kernel != "6.8.0" || node.Arch != "x86_64" ||
		node.Virtualization != "kvm guest" || node.AgentVersion != "1.2.3" {
		t.Fatalf("node system info = %#v", node)
	}

	heartbeat.System.AgentVersion = "1.2.4"
	updated, err := nodeStore.UpsertNodeWithSystem(context.Background(), heartbeat)
	if err != nil {
		t.Fatalf("update node system info: %v", err)
	}
	if updated.ID != node.ID || updated.AgentVersion != "1.2.4" {
		t.Fatalf("updated node = %#v", updated)
	}
}

func TestUpsertNodeNewAssignsDefaultGroup(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	groupStore := NewGroupStore(s.DB)

	defaultGroup, err := groupStore.GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("get default group: %v", err)
	}

	node, err := nodeStore.UpsertNode(ctx, "assigned-node", "10.0.0.31", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if node.GroupID != defaultGroup.ID {
		t.Fatalf("group ID = %q, want %q", node.GroupID, defaultGroup.ID)
	}
}

func TestUpsertNodeExisting(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first, err := nodeStore.UpsertNode(ctx, "upsert-existing", "10.0.0.40", 1111)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := nodeStore.UpsertNode(ctx, "upsert-existing", "10.0.0.41", 2222)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("expected same ID after upsert, got %q and %q", first.ID, second.ID)
	}
	if second.Host != "10.0.0.41" {
		t.Errorf("expected host %q, got %q", "10.0.0.41", second.Host)
	}
	if second.Port != 2222 {
		t.Errorf("expected port %d, got %d", 2222, second.Port)
	}
	if second.Status != model.NodeStatusOnline {
		t.Errorf("expected status %q, got %q", model.NodeStatusOnline, second.Status)
	}
}

func TestListOnlineNodes(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(store.DB)

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"online-1", "online-node", "", "", "10.0.0.50", 22, model.NodeStatusOnline, "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, alias, group_id, host, port, status, ssh_public_key, last_seen, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"offline-1", "offline-node", "", "", "10.0.0.51", 22, model.NodeStatusOffline, "", model.NowUTC(), model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodes, err := nodeStore.ListOnlineNodes(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("expected 1 online node, got %d", len(nodes))
	}
	if nodes[0].Name != "online-node" {
		t.Errorf("expected name %q, got %q", "online-node", nodes[0].Name)
	}
}
