package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func setupTestDB(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestCreateGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	g, err := groupStore.CreateGroup(ctx, "Test Group")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g == nil {
		t.Fatal("expected group, got nil")
	}
	if g.Name != "Test Group" {
		t.Errorf("expected name %q, got %q", "Test Group", g.Name)
	}
	if g.IsDefault {
		t.Error("expected IsDefault to be false")
	}
	if g.ID == "" {
		t.Error("expected group ID to be non-empty")
	}

	groups, err := groupStore.ListGroups(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, grp := range groups {
		if grp.ID == g.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("created group not found in list")
	}
}

func TestListGroups(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	names := []string{"C Group", "A Group", "B Group"}
	for i, name := range names {
		_, err := groupStore.CreateGroup(ctx, name)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if i == 0 {
			_ = groupStore.UpdateSortOrder(ctx, []string{
				"_placeholder_",
			})
			_ = store.DB.Close()
			store = setupTestDB(t)
			ctx = context.Background()
			groupStore = NewGroupStore(store.DB)
			_, err = groupStore.CreateGroup(ctx, "A Group")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			_, err = groupStore.CreateGroup(ctx, "B Group")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}
}

func TestListGroupsOrdering(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	g1, _ := groupStore.CreateGroup(ctx, "First")
	g2, _ := groupStore.CreateGroup(ctx, "Second")
	g3, _ := groupStore.CreateGroup(ctx, "Third")

	_ = g1
	_ = g2
	_ = g3

	groups, err := groupStore.ListGroups(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(groups) < 4 {
		t.Fatalf("expected at least 4 groups (1 default + 3 created), got %d", len(groups))
	}

	prevOrder := -1
	for _, g := range groups {
		if g.SortOrder < prevOrder {
			t.Errorf("groups not sorted: %d came after %d", g.SortOrder, prevOrder)
		}
		prevOrder = g.SortOrder
	}
}

func TestGetGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	created, err := groupStore.CreateGroup(ctx, "Get Test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := groupStore.GetGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected group, got nil")
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
	if got.Name != "Get Test" {
		t.Errorf("expected name %q, got %q", "Get Test", got.Name)
	}

	got, err = groupStore.GetGroup(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent group")
	}
}

func TestUpdateGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	created, err := groupStore.CreateGroup(ctx, "Old Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = groupStore.UpdateGroup(ctx, created.ID, "New Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := groupStore.GetGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("expected name %q, got %q", "New Name", updated.Name)
	}
}

func TestUpdateGroupNotFound(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	err := groupStore.UpdateGroup(ctx, "nonexistent-id", "New Name")
	if err == nil {
		t.Fatal("expected error for updating non-existent group")
	}
}

func TestDeleteGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	created, err := groupStore.CreateGroup(ctx, "To Delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = groupStore.DeleteGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := groupStore.GetGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteDefaultGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	defaultGroup, err := groupStore.GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = groupStore.DeleteGroup(ctx, defaultGroup.ID)
	if err != ErrDefaultGroupDelete {
		t.Errorf("expected ErrDefaultGroupDelete, got %v", err)
	}
}

func TestDeleteGroupNodesMoved(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	created, err := groupStore.CreateGroup(ctx, "Group With Node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defaultGroup, err := groupStore.GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = store.DB.ExecContext(ctx,
		"INSERT INTO nodes (id, name, group_id, host, port, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"test-node-id", "test-node", created.ID, "192.168.0.1", 22, "offline", model.NowUTC(), model.NowUTC(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := store.DB.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = groupStore.DeleteGroup(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var groupID string
	err = store.DB.QueryRowContext(ctx, "SELECT group_id FROM nodes WHERE id = ?", "test-node-id").Scan(&groupID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if groupID != defaultGroup.ID {
		t.Errorf("expected node to be in default group %q, got %q", defaultGroup.ID, groupID)
	}
}

func TestGetDefaultGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	defaultGroup, err := groupStore.GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defaultGroup == nil {
		t.Fatal("expected default group to exist after init")
	}
	if defaultGroup.Name != "Default" {
		t.Errorf("expected name %q, got %q", "Default", defaultGroup.Name)
	}
	if !defaultGroup.IsDefault {
		t.Error("expected IsDefault to be true")
	}
}

func TestSetDefaultGroup(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	oldDefault, err := groupStore.GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newGroup, err := groupStore.CreateGroup(ctx, "New Default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = groupStore.SetDefaultGroup(ctx, newGroup.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newDefault, err := groupStore.GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newDefault.ID != newGroup.ID {
		t.Errorf("expected default group %q, got %q", newGroup.ID, newDefault.ID)
	}
	if newDefault.Name != "New Default" {
		t.Errorf("expected name %q, got %q", "New Default", newDefault.Name)
	}

	oldGot, err := groupStore.GetGroup(ctx, oldDefault.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oldGot.IsDefault {
		t.Error("expected old default group to no longer be default")
	}
}

func TestSetDefaultGroupNotFound(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	err := groupStore.SetDefaultGroup(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for setting non-existent default group")
	}
}

func TestUpdateSortOrder(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	g1, _ := groupStore.CreateGroup(ctx, "Group 1")
	g2, _ := groupStore.CreateGroup(ctx, "Group 2")
	g3, _ := groupStore.CreateGroup(ctx, "Group 3")

	newOrder := []string{g3.ID, g1.ID, g2.ID}
	err := groupStore.UpdateSortOrder(ctx, newOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	groups, err := groupStore.ListGroups(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reordered := map[string]bool{
		g1.ID: true,
		g2.ID: true,
		g3.ID: true,
	}
	var filtered []string
	for _, g := range groups {
		if reordered[g.ID] {
			filtered = append(filtered, g.ID)
		}
	}

	for i, id := range newOrder {
		if filtered[i] != id {
			t.Errorf("at position %d expected group %q, got %q", i, id, filtered[i])
		}
	}
}

func TestCreateGroupInvalidName(t *testing.T) {
	store := setupTestDB(t)
	ctx := context.Background()
	groupStore := NewGroupStore(store.DB)

	g, err := groupStore.CreateGroup(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.Name != "" {
		t.Errorf("expected empty name, got %q", g.Name)
	}
}

func TestGroupBoolToInt(t *testing.T) {
	t.Run("true_to_1", func(t *testing.T) {
		result := boolToInt(true)
		if result != 1 {
			t.Errorf("expected 1, got %d", result)
		}
	})
	t.Run("false_to_0", func(t *testing.T) {
		result := boolToInt(false)
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}
