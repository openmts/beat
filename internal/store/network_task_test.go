package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestNetworkTaskStoreCRUDAndAssignments(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	nodeStore := NewNodeStore(s.DB)
	first, err := nodeStore.UpsertNode(ctx, "node-one", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create first node: %v", err)
	}
	second, err := nodeStore.UpsertNode(ctx, "node-two", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create second node: %v", err)
	}
	store := NewNetworkTaskStore(s.DB)
	task, err := store.CreateTask(ctx, validNetworkTask(), []string{first.ID})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if task.ID == "" || len(task.Nodes) != 1 || task.Nodes[0].Name != "node-one" {
		t.Fatalf("created task = %#v", task)
	}
	assignments, err := store.ListAssignments(ctx, first.ID)
	if err != nil || len(assignments) != 1 || assignments[0].Name != task.Name {
		t.Fatalf("first assignments = %#v, error = %v", assignments, err)
	}
	assignments, err = store.ListAssignments(ctx, second.ID)
	if err != nil || len(assignments) != 0 {
		t.Fatalf("second assignments = %#v, error = %v", assignments, err)
	}
	effective, err := store.ListEffectiveTaskNodes(ctx, task.ID, false)
	if err != nil || len(effective) != 1 || effective[0].ID != first.ID {
		t.Fatalf("explicit effective nodes = %#v, error = %v", effective, err)
	}
	assigned, err := store.IsTaskAssignedToNode(ctx, task.ID, first.ID)
	if err != nil || !assigned {
		t.Fatalf("first node assigned = %v, error = %v", assigned, err)
	}
	assigned, err = store.IsTaskAssignedToNode(ctx, task.ID, second.ID)
	if err != nil || assigned {
		t.Fatalf("second node assigned = %v, error = %v", assigned, err)
	}

	updatedInput := *task
	updatedInput.AllNodes = true
	updatedInput.Name = "All nodes"
	updated, err := store.UpdateTask(ctx, task.ID, updatedInput, []string{second.ID})
	if err != nil {
		t.Fatalf("update task: %v", err)
	}
	if updated == nil || updated.Name != "All nodes" || len(updated.Nodes) != 0 {
		t.Fatalf("updated task = %#v", updated)
	}
	assignments, err = store.ListAssignments(ctx, second.ID)
	if err != nil || len(assignments) != 1 {
		t.Fatalf("all-node assignments = %#v, error = %v", assignments, err)
	}
	effective, err = store.ListEffectiveTaskNodes(ctx, task.ID, true)
	if err != nil || len(effective) != 2 {
		t.Fatalf("all effective nodes = %#v, error = %v", effective, err)
	}
	assigned, err = store.IsTaskAssignedToNode(ctx, task.ID, second.ID)
	if err != nil || !assigned {
		t.Fatalf("all-node assignment = %v, error = %v", assigned, err)
	}
	assigned, err = store.IsTaskAssignedToNode(ctx, "missing", second.ID)
	if err != nil || assigned {
		t.Fatalf("missing task assignment = %v, error = %v", assigned, err)
	}

	deleted, err := store.DeleteTask(ctx, task.ID)
	if err != nil || !deleted {
		t.Fatalf("delete task = %v, error = %v", deleted, err)
	}
	got, err := store.GetTask(ctx, task.ID)
	if err != nil || got != nil {
		t.Fatalf("deleted task = %#v, error = %v", got, err)
	}
}

func TestNetworkTaskStorePublicSortAndCascade(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	node, err := NewNodeStore(s.DB).UpsertNode(ctx, "public-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	store := NewNetworkTaskStore(s.DB)
	publicTask := validNetworkTask()
	publicTask.Name = "Public"
	publicTask.IsPublic = true
	publicTask.SortOrder = 5
	createdPublic, err := store.CreateTask(ctx, publicTask, []string{node.ID})
	if err != nil {
		t.Fatalf("create public task: %v", err)
	}
	privateTask := validNetworkTask()
	privateTask.Name = "Private"
	createdPrivate, err := store.CreateTask(ctx, privateTask, []string{node.ID})
	if err != nil {
		t.Fatalf("create private task: %v", err)
	}
	public, err := store.ListTasks(ctx, true)
	if err != nil || len(public) != 1 || public[0].ID != createdPublic.ID {
		t.Fatalf("public tasks = %#v, error = %v", public, err)
	}
	if err := store.UpdateSortOrder(ctx, []string{createdPublic.ID, createdPrivate.ID}); err != nil {
		t.Fatalf("sort tasks: %v", err)
	}
	all, err := store.ListTasks(ctx, false)
	if err != nil || len(all) != 2 || all[0].ID != createdPublic.ID {
		t.Fatalf("sorted tasks = %#v, error = %v", all, err)
	}
	if err := NewNodeStore(s.DB).DeleteNode(ctx, node.ID); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	got, err := store.GetTask(ctx, createdPublic.ID)
	if err != nil || got == nil || len(got.Nodes) != 0 {
		t.Fatalf("task after node cascade = %#v, error = %v", got, err)
	}
}

func TestNetworkTaskStorePublicEffectiveNodesExcludeHidden(t *testing.T) {
	s := setupTestDB(t)
	ctx := t.Context()
	nodes := NewNodeStore(s.DB)
	visible, err := nodes.UpsertNode(ctx, "visible-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create visible node: %v", err)
	}
	hidden, err := nodes.UpsertNode(ctx, "hidden-node", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create hidden node: %v", err)
	}
	isPublic := false
	if _, err := nodes.UpdateNode(ctx, hidden.ID, NodeUpdate{
		Alias: hidden.Alias, GroupID: hidden.GroupID, SSHPublicKey: hidden.SSHPublicKey,
		IsPublic: &isPublic,
	}); err != nil {
		t.Fatalf("hide node: %v", err)
	}

	tasks := NewNetworkTaskStore(s.DB)
	task, err := tasks.CreateTask(ctx, validNetworkTask(), []string{visible.ID, hidden.ID})
	if err != nil {
		t.Fatalf("create explicit task: %v", err)
	}
	explicit, err := tasks.ListEffectivePublicTaskNodes(ctx, task.ID, false)
	if err != nil || len(explicit) != 1 || explicit[0].ID != visible.ID {
		t.Fatalf("public explicit nodes = %#v, error = %v", explicit, err)
	}
	all, err := tasks.ListEffectivePublicTaskNodes(ctx, task.ID, true)
	if err != nil || len(all) != 1 || all[0].ID != visible.ID {
		t.Fatalf("public all nodes = %#v, error = %v", all, err)
	}
}

func TestNetworkTaskStoreRejectsInvalidWrites(t *testing.T) {
	s := setupTestDB(t)
	store := NewNetworkTaskStore(s.DB)
	task := validNetworkTask()
	if _, err := store.CreateTask(context.Background(), task, nil); err == nil {
		t.Fatal("expected explicit assignment error")
	}
	task.AllNodes = true
	if _, err := store.CreateTask(context.Background(), task, []string{"missing"}); err != nil {
		t.Fatalf("all-node task should ignore node IDs: %v", err)
	}
	if err := store.UpdateSortOrder(context.Background(), []string{"missing"}); err == nil {
		t.Fatal("expected unknown sort ID error")
	}
}

func TestNetworkTaskStoreValidationAndMissingRecords(t *testing.T) {
	s := setupTestDB(t)
	ctx := t.Context()
	node, err := NewNodeStore(s.DB).UpsertNode(ctx, "validation-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	tasks := NewNetworkTaskStore(s.DB)
	invalid := validNetworkTask()
	invalid.Name = ""
	if _, err := tasks.CreateTask(ctx, invalid, []string{node.ID}); err == nil {
		t.Fatal("expected invalid task error")
	}
	for _, nodeIDs := range [][]string{{""}, {node.ID, node.ID}} {
		if _, err := tasks.CreateTask(ctx, validNetworkTask(), nodeIDs); err == nil {
			t.Fatalf("expected invalid node IDs error for %#v", nodeIDs)
		}
	}
	if _, err := tasks.CreateTask(ctx, validNetworkTask(), []string{"missing"}); err == nil {
		t.Fatal("expected unknown node error")
	}

	allNodes := validNetworkTask()
	allNodes.AllNodes = true
	created, err := tasks.CreateTask(ctx, allNodes, nil)
	if err != nil {
		t.Fatalf("create all-node task: %v", err)
	}
	missing, err := tasks.UpdateTask(ctx, "missing", allNodes, nil)
	if err != nil || missing != nil {
		t.Fatalf("update missing task = %#v, error = %v", missing, err)
	}
	updated := *created
	updated.AllNodes = false
	if _, err := tasks.UpdateTask(ctx, created.ID, updated, []string{"missing"}); err == nil {
		t.Fatal("expected unknown update node error")
	}
	deleted, err := tasks.DeleteTask(ctx, "missing")
	if err != nil || deleted {
		t.Fatalf("delete missing task = %v, error = %v", deleted, err)
	}
}

func TestNetworkTaskStoreClosedDatabaseErrors(t *testing.T) {
	s := setupTestDB(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	ctx := t.Context()
	tasks := NewNetworkTaskStore(s.DB)
	task := validNetworkTask()
	task.AllNodes = true

	checks := []struct {
		name string
		run  func() error
	}{
		{name: "create", run: func() error { _, err := tasks.CreateTask(ctx, task, nil); return err }},
		{name: "update", run: func() error { _, err := tasks.UpdateTask(ctx, "task", task, nil); return err }},
		{name: "get", run: func() error { _, err := tasks.GetTask(ctx, "task"); return err }},
		{name: "list", run: func() error { _, err := tasks.ListTasks(ctx, false); return err }},
		{name: "delete", run: func() error { _, err := tasks.DeleteTask(ctx, "task"); return err }},
		{name: "sort", run: func() error { return tasks.UpdateSortOrder(ctx, []string{"task"}) }},
		{name: "task nodes", run: func() error { _, err := tasks.listTaskNodes(ctx, "task"); return err }},
		{name: "assignments", run: func() error { _, err := tasks.ListAssignments(ctx, "node"); return err }},
		{name: "assigned task", run: func() error { _, err := tasks.GetAssignedTask(ctx, "node", "task"); return err }},
		{name: "effective nodes", run: func() error { _, err := tasks.ListEffectiveTaskNodes(ctx, "task", true); return err }},
		{name: "public effective nodes", run: func() error {
			_, err := tasks.ListEffectivePublicTaskNodes(ctx, "task", true)
			return err
		}},
		{name: "assigned node", run: func() error { _, err := tasks.IsTaskAssignedToNode(ctx, "task", "node"); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); err == nil {
				t.Fatal("expected closed database error")
			}
		})
	}
}

func TestNetworkTaskStoreAssignedTaskState(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()
	node, err := NewNodeStore(s.DB).UpsertNode(ctx, "result-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	store := NewNetworkTaskStore(s.DB)
	task, err := store.CreateTask(ctx, validNetworkTask(), []string{node.ID})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	assigned, err := store.GetAssignedTask(ctx, node.ID, task.ID)
	if err != nil || assigned == nil || !assigned.Assigned || !assigned.Enabled {
		t.Fatalf("assigned task = %#v, error = %v", assigned, err)
	}
	missing, err := store.GetAssignedTask(ctx, "missing", task.ID)
	if err != nil || missing != nil {
		t.Fatalf("missing assignment = %#v, error = %v", missing, err)
	}
}

func validNetworkTask() model.NetworkTask {
	return model.NetworkTask{
		Name: "Loopback", Type: model.NetworkProbeICMP, Target: "127.0.0.1",
		IPFamily: model.IPFamilyAuto, IntervalSeconds: 60, TimeoutMilliseconds: 1000,
		Enabled: true,
	}
}
