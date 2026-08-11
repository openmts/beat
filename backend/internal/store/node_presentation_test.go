package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestNodePresentationVisibilityAndOrdering(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	group, err := NewGroupStore(sqliteStore.DB).GetDefaultGroup(t.Context())
	if err != nil {
		t.Fatalf("get default group: %v", err)
	}
	now := model.NowUTC()
	fixtures := []struct {
		id       string
		name     string
		order    int
		isPublic bool
	}{
		{id: "node-b", name: "bravo", order: 0, isPublic: true},
		{id: "node-hidden", name: "hidden", order: 1, isPublic: false},
		{id: "node-a", name: "alpha", order: 2, isPublic: true},
	}
	for _, fixture := range fixtures {
		_, err := sqliteStore.DB.ExecContext(t.Context(), `INSERT INTO nodes
			(id, name, alias, group_id, host, port, status, ssh_public_key, sort_order,
			 tags, is_public, public_remark, private_remark, created_at, updated_at)
			 VALUES (?, ?, '', ?, '127.0.0.1', 22, 'offline', '', ?, '[]', ?, '', '', ?, ?)`,
			fixture.id, fixture.name, group.ID, fixture.order, fixture.isPublic, now, now)
		if err != nil {
			t.Fatalf("insert %s: %v", fixture.name, err)
		}
	}

	publicNodes, err := nodes.ListPublicNodes(t.Context(), "")
	if err != nil {
		t.Fatalf("list public nodes: %v", err)
	}
	if len(publicNodes) != 2 || publicNodes[0].Name != "bravo" || publicNodes[1].Name != "alpha" {
		t.Fatalf("public nodes = %#v", publicNodes)
	}
	if node, err := nodes.GetPublicNode(t.Context(), "node-hidden"); err != nil || node != nil {
		t.Fatalf("hidden public node = %#v, err = %v", node, err)
	}
	groupNodes, err := nodes.ListPublicNodes(t.Context(), group.ID)
	if err != nil || len(groupNodes) != 2 {
		t.Fatalf("public group nodes = %#v, err = %v", groupNodes, err)
	}
	visible, err := nodes.GetPublicNode(t.Context(), "node-b")
	if err != nil || visible == nil || visible.Name != "bravo" {
		t.Fatalf("visible public node = %#v, err = %v", visible, err)
	}
}

func TestNodePresentationClosedDatabaseErrors(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if _, err := nodes.ListPublicNodes(t.Context(), "group"); err == nil {
		t.Fatal("expected public list error")
	}
	if _, err := nodes.GetPublicNode(t.Context(), "node"); err == nil {
		t.Fatal("expected public node error")
	}
	if err := nodes.UpdateNodeSort(t.Context(), []string{"node"}); err == nil {
		t.Fatal("expected node sort error")
	}
}

func TestUpdateNodePresentation(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "presentation", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	tags := []string{"edge", "prod"}
	isPublic := false
	sortOrder := 4
	publicRemark := "Public note"
	privateRemark := "Private note"
	updated, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{
		Alias: node.Alias, GroupID: node.GroupID, SSHPublicKey: node.SSHPublicKey,
		Tags: &tags, IsPublic: &isPublic, SortOrder: &sortOrder,
		PublicRemark: &publicRemark, PrivateRemark: &privateRemark,
	})
	if err != nil {
		t.Fatalf("update presentation: %v", err)
	}
	if updated.IsPublic || updated.SortOrder != 4 || updated.PublicRemark != publicRemark ||
		updated.PrivateRemark != privateRemark || len(updated.Tags) != 2 {
		t.Fatalf("updated node = %#v", updated)
	}
}

func TestUpdateNodePresentationStoresEmptyTagsArray(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "empty-tags", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	tags := []string{}
	updated, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{
		Alias: node.Alias, GroupID: node.GroupID, SSHPublicKey: node.SSHPublicKey,
		Tags: &tags,
	})
	if err != nil {
		t.Fatalf("update empty tags: %v", err)
	}
	if updated.Tags == nil || len(updated.Tags) != 0 {
		t.Fatalf("updated tags = %#v", updated.Tags)
	}
	var stored string
	if err := sqliteStore.DB.QueryRowContext(t.Context(),
		"SELECT tags FROM nodes WHERE id = ?", node.ID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored tags: %v", err)
	}
	if stored != "[]" {
		t.Fatalf("stored tags = %q, want []", stored)
	}
}

func TestUpdateNodeSort(t *testing.T) {
	sqliteStore := setupTestDB(t)
	nodes := NewNodeStore(sqliteStore.DB)
	first, err := nodes.UpsertNode(t.Context(), "first", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := nodes.UpsertNode(t.Context(), "second", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := nodes.UpdateNodeSort(t.Context(), []string{second.ID, first.ID}); err != nil {
		t.Fatalf("update node sort: %v", err)
	}
	listed, err := nodes.ListNodes(t.Context(), "")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != second.ID || listed[1].ID != first.ID {
		t.Fatalf("sorted nodes = %#v", listed)
	}

	otherGroup, err := NewGroupStore(sqliteStore.DB).CreateGroup(t.Context(), "Other")
	if err != nil {
		t.Fatalf("create other group: %v", err)
	}
	third, err := nodes.UpsertNode(t.Context(), "third", "127.0.0.3", 22)
	if err != nil {
		t.Fatalf("create third: %v", err)
	}
	if err := nodes.UpdateNodeSort(t.Context(), []string{first.ID, second.ID}); !errors.Is(err, ErrInvalidNodeSort) {
		t.Fatalf("partial sort error = %v", err)
	}
	if _, err := nodes.UpdateNode(t.Context(), third.ID, NodeUpdate{GroupID: otherGroup.ID}); err != nil {
		t.Fatalf("move third: %v", err)
	}
	if err := nodes.UpdateNodeSort(t.Context(), []string{first.ID, third.ID}); !errors.Is(err, ErrInvalidNodeSort) {
		t.Fatalf("cross-group sort error = %v", err)
	}
	if err := nodes.UpdateNodeSort(t.Context(), []string{first.ID, first.ID}); !errors.Is(err, ErrInvalidNodeSort) {
		t.Fatalf("duplicate sort error = %v", err)
	}
	for name, ids := range map[string][]string{
		"empty":   nil,
		"blank":   {""},
		"missing": {"missing"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := nodes.UpdateNodeSort(t.Context(), ids); !errors.Is(err, ErrInvalidNodeSort) {
				t.Fatalf("sort error = %v", err)
			}
		})
	}
	tooMany := make([]string, maxNodeSortItems+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("node-%d", index)
	}
	if err := nodes.UpdateNodeSort(t.Context(), tooMany); !errors.Is(err, ErrInvalidNodeSort) {
		t.Fatalf("oversized sort error = %v", err)
	}
}
