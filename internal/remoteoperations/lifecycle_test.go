package remoteoperations

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

func TestNodeDeletionGuardsAndForceRetainsUnknownEvidence(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "node-delete")
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := store.NewNodeStore(f.database.DB).DeleteNode(t.Context(), f.nodeID); !errors.Is(err, store.ErrOperationNodeBusy) {
		t.Fatal(err)
	}
	input := ForceNodeDelete{NodeID: f.nodeID, Reason: "host is unreachable"}
	if err := f.service.ForceDeleteNode(t.Context(), f.actor, input); err != ErrInvalid {
		t.Fatal(err)
	}
	input.AcknowledgeUnknownRisk = true
	if err := f.service.ForceDeleteNode(t.Context(), f.actor, input); err != nil {
		t.Fatal(err)
	}
	task, err := f.service.GetTask(t.Context(), f.actor, id)
	if err != nil || task.State != "unknown" || task.Targets[0].NodeID != "" || task.Targets[0].NodeName != "fixture" {
		t.Fatal(task, err)
	}
	if err := f.service.Start(t.Context(), request); err != ErrForbidden {
		t.Fatal("deleted credential retained authority", err)
	}
	var state string
	if err := f.database.DB.QueryRow("SELECT state FROM operation_attempts WHERE id=?", request.ID).Scan(&state); err != nil || state != "unknown" {
		t.Fatal("claimed an unreachable process stopped", state, err)
	}
}

func TestCompletedNodeDeletionKeepsOutputAndNameSnapshot(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "terminal-delete")
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded", Stdout: []byte("retained")}}); err != nil {
		t.Fatal(err)
	}
	if err := store.NewNodeStore(f.database.DB).DeleteNode(t.Context(), f.nodeID); err != nil {
		t.Fatal(err)
	}
	task, err := f.service.GetTask(t.Context(), f.actor, id)
	if err != nil || task.Targets[0].NodeName != "fixture" || task.Targets[0].NodeID != "" {
		t.Fatal(task, err)
	}
	output, err := f.service.TargetOutput(t.Context(), f.actor, TargetRequest{TaskID: id, TargetID: task.Targets[0].ID})
	if err != nil || output.AttemptID != request.ID || string(output.Stdout) != "retained" {
		t.Fatal(output, err)
	}
}

func TestEnabledPolicyPreventsGroupDeletionWithoutMovingNodes(t *testing.T) {
	f := testFixture(t)
	now := f.service.now()
	f.sql(t, "INSERT INTO groups(id,name,is_default,created_at,updated_at) VALUES('ops-group','Operations',0,?,?)", now, now)
	f.sql(t, "UPDATE nodes SET group_id='ops-group' WHERE id=?", f.nodeID)
	if _, err := f.service.SavePolicy(t.Context(), f.actor, PolicyInput{ID: f.policyID, Version: 1, Name: "Scoped policy", Policy: f.policy, GroupIDs: []string{"ops-group"}}); err != nil {
		t.Fatal(err)
	}
	err := store.NewGroupStore(f.database.DB).DeleteGroup(t.Context(), "ops-group")
	if !errors.Is(err, store.ErrOperationGroupBusy) {
		t.Fatal(err)
	}
	var group string
	if err := f.database.DB.QueryRow("SELECT group_id FROM nodes WHERE id=?", f.nodeID).Scan(&group); err != nil || group != "ops-group" {
		t.Fatal("failed deletion moved nodes", group, err)
	}
	if err := f.service.DisablePolicy(t.Context(), f.actor, f.policyID); err != nil {
		t.Fatal(err)
	}
	if err := store.NewGroupStore(f.database.DB).DeleteGroup(t.Context(), "ops-group"); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionRemovesCiphertextThenTaskMetadata(t *testing.T) {
	f := testFixture(t)
	id := f.task(t, "retention")
	request := f.lease(t)
	if err := f.service.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Complete(t.Context(), Completion{Attempt: request, Result: Result{State: "succeeded", Stdout: []byte("expires")}}); err != nil {
		t.Fatal(err)
	}
	now := f.service.now().Add(8 * 24 * time.Hour)
	f.service.now = func() time.Time { return now }
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_attempts WHERE stdout_encrypted IS NOT NULL").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	now = now.Add(30 * 24 * time.Hour)
	if err := f.service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.database.DB.QueryRow("SELECT COUNT(*) FROM operation_tasks WHERE id=?", id).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestTaskPagingCountsCanonicalTargetsAndContentAdmission(t *testing.T) {
	f := testFixture(t)
	for _, key := range []string{"one", "two", "three"} {
		f.task(t, key)
	}
	page, err := f.service.ListTaskPage(t.Context(), f.actor, TaskPageQuery{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" || page.Items[0].State != "queued" || page.Items[0].TargetCount != 1 {
		t.Fatal(page, err)
	}
	last, err := f.service.ListTaskPage(t.Context(), f.actor, TaskPageQuery{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatal(last, err)
	}
	if _, err := f.service.ListTaskPage(t.Context(), f.actor, TaskPageQuery{Cursor: "invalid"}); err != ErrInvalid {
		t.Fatal(err)
	}
	content, err := f.service.TaskContent(t.Context(), f.actor, page.Items[0].ID)
	if err != nil || content.Payload.Limits.Targets != 1 || len(content.Payload.Command) == 0 {
		t.Fatal(content, err)
	}
	if _, err := f.service.TaskContent(t.Context(), Actor{}, content.ID); err != ErrForbidden {
		t.Fatal(err)
	}
	f.service.enabled = false
	status, err := f.service.Status(t.Context(), f.actor)
	if err != nil || status.Enabled {
		t.Fatal(status, err)
	}
}
