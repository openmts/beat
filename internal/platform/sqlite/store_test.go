package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"beat/internal/domain"
)

func TestSystemSettingRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.GetSetting(ctx, "system"); err != nil || ok {
		t.Fatalf("GetSetting() missing = %v, %v; want false, nil", ok, err)
	}
	if err := store.SetSetting(ctx, "system", `{"site":{"name":"Ops"}}`); err != nil {
		t.Fatal(err)
	}
	value, ok, err := store.GetSetting(ctx, "system")
	if err != nil || !ok {
		t.Fatalf("GetSetting() = %q, %v, %v; want found", value, ok, err)
	}
	if value != `{"site":{"name":"Ops"}}` {
		t.Fatalf("GetSetting() value = %q", value)
	}
	if err := store.SetSetting(ctx, "system", `{"site":{"name":"NOC"}}`); err != nil {
		t.Fatal(err)
	}
	value, _, err = store.GetSetting(ctx, "system")
	if err != nil || value != `{"site":{"name":"NOC"}}` {
		t.Fatalf("updated GetSetting() = %q, %v", value, err)
	}
}

func TestEnrollmentTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	if err := store.CreateEnrollmentToken(ctx, "token-1", "beat_enroll_test", "edge-node-01", now, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	authorized, err := store.AuthorizeEnrollmentToken(ctx, "beat_enroll_test", "agent-a", now.Add(time.Minute))
	if err != nil || !authorized {
		t.Fatalf("first authorization = %v, %v; want true, nil", authorized, err)
	}
	authorized, err = store.AuthorizeEnrollmentToken(ctx, "beat_enroll_test", "agent-b", now.Add(2*time.Minute))
	if err != nil || authorized {
		t.Fatalf("other agent authorization = %v, %v; want false, nil", authorized, err)
	}
	authorized, err = store.AuthorizeEnrollmentToken(ctx, "beat_enroll_test", "agent-a", now.Add(30*time.Minute))
	if err != nil || !authorized {
		t.Fatalf("bound agent authorization after TTL = %v, %v; want true, nil", authorized, err)
	}

	if err := store.CreateEnrollmentToken(ctx, "token-2", "beat_enroll_expired", "edge-node-02", now, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	authorized, err = store.AuthorizeEnrollmentToken(ctx, "beat_enroll_expired", "agent-c", now.Add(11*time.Minute))
	if err != nil || authorized {
		t.Fatalf("expired unbound authorization = %v, %v; want false, nil", authorized, err)
	}

	authorized, err = store.AuthorizeEnrollmentToken(ctx, "beat_enroll_missing", "agent-a", now)
	if err != nil || authorized {
		t.Fatalf("unknown token authorization = %v, %v; want false, nil", authorized, err)
	}
}

func TestNodeConfigOverrideAndUninstallLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	report := domain.Report{
		AgentID: "agent-lifecycle", Hostname: "reported-edge-01", Address: "10.0.0.10",
		OS: "Linux", Kernel: "6.8", Arch: "amd64", AgentVersion: "test", IntervalSeconds: 15,
		Labels: map[string]string{"role": "edge"}, Metrics: domain.Metrics{Timestamp: now, CPUUsage: 12},
	}
	node, err := store.UpsertNode(ctx, report, now)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateNodeConfig(ctx, node.ID, "edge-production-01", map[string]string{"role": "gateway", "region": "cn-east"}, "", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Hostname != "edge-production-01" || updated.Labels["role"] != "gateway" {
		t.Fatalf("updated node = %+v", updated)
	}

	if _, err := store.UpsertNode(ctx, report, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	afterReport, err := store.GetNode(ctx, node.ID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if afterReport.Hostname != "edge-production-01" || afterReport.Labels["region"] != "cn-east" {
		t.Fatalf("node config was overwritten by report: %+v", afterReport)
	}

	if err := store.CreateEnrollmentToken(ctx, "token-lifecycle", "beat_enroll_lifecycle", "edge", now, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	authorized, err := store.AuthorizeEnrollmentToken(ctx, "beat_enroll_lifecycle", report.AgentID, now.Add(time.Minute))
	if err != nil || !authorized {
		t.Fatalf("bind token = %v, %v", authorized, err)
	}

	removedNode, task, err := store.UninstallNode(ctx, node.ID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if removedNode.Hostname != "edge-production-01" || removedNode.Labels["role"] != "gateway" {
		t.Fatalf("removed node did not use configured values: %+v", removedNode)
	}
	if task.NodeID != node.ID {
		t.Fatalf("task node = %q, want %q", task.NodeID, node.ID)
	}
	if task.Kind != "uninstall_agent" || task.Status != "pending" || task.AgentID != report.AgentID {
		t.Fatalf("task = %+v", task)
	}
	if deleted, err := store.IsAgentDeleted(ctx, report.AgentID); err != nil || !deleted {
		t.Fatalf("IsAgentDeleted() = %v, %v", deleted, err)
	}
	if _, err := store.GetNode(ctx, node.ID, now); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetNode() after uninstall error = %v, want sql.ErrNoRows", err)
	}

	tasks, err := store.ClaimAgentTasks(ctx, report.AgentID, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Status != "delivered" {
		t.Fatalf("ClaimAgentTasks() = %+v", tasks)
	}
	completed, err := store.CompleteAgentTask(ctx, task.ID, report.AgentID, "success", "removed", now.Add(5*time.Minute))
	if err != nil || !completed {
		t.Fatalf("CompleteAgentTask() = %v, %v", completed, err)
	}
	authorized, err = store.AuthorizeEnrollmentToken(ctx, "beat_enroll_lifecycle", report.AgentID, now.Add(6*time.Minute))
	if err != nil || authorized {
		t.Fatalf("authorization after uninstall = %v, %v; want false, nil", authorized, err)
	}
}

func TestNodeGroupsAndMembership(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	edgeGroup, err := store.CreateGroup(ctx, "group-edge", "边缘接入", "华东边缘节点", "blue", now)
	if err != nil {
		t.Fatal(err)
	}
	if edgeGroup.NodeCount != 0 {
		t.Fatalf("new group node count = %d, want 0", edgeGroup.NodeCount)
	}
	if _, err := store.CreateGroup(ctx, "group-duplicate", "边缘接入", "", "cyan", now); !errors.Is(err, ErrGroupNameConflict) {
		t.Fatalf("duplicate group error = %v, want ErrGroupNameConflict", err)
	}

	first, err := store.UpsertNode(ctx, domain.Report{
		AgentID: "agent-group-01", Hostname: "edge-01", Address: "10.0.0.1",
		OS: "Linux", Arch: "amd64", IntervalSeconds: 15, Metrics: domain.Metrics{CPUUsage: 20},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.UpsertNode(ctx, domain.Report{
		AgentID: "agent-group-02", Hostname: "edge-02", Address: "10.0.0.2",
		OS: "Linux", Arch: "amd64", IntervalSeconds: 15, Metrics: domain.Metrics{CPUUsage: 30},
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetGroupMembers(ctx, edgeGroup.ID, []string{first.ID, second.ID}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	edgeGroup, err = store.GetGroup(ctx, edgeGroup.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if edgeGroup.NodeCount != 2 || edgeGroup.OnlineCount != 2 {
		t.Fatalf("group counts = %+v, want 2 nodes and 2 online", edgeGroup)
	}
	assigned, err := store.GetNode(ctx, first.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if assigned.GroupID != edgeGroup.ID || assigned.GroupName != "边缘接入" || assigned.GroupColor != "blue" {
		t.Fatalf("assigned node group = %+v", assigned)
	}

	if _, err := store.UpdateNodeConfig(ctx, first.ID, "edge-01-renamed", map[string]string{"role": "edge"}, edgeGroup.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetGroupMembers(ctx, edgeGroup.ID, []string{second.ID}, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ungrouped, err := store.GetNode(ctx, first.ID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ungrouped.GroupID != "" || ungrouped.GroupName != "" {
		t.Fatalf("removed member still has group: %+v", ungrouped)
	}

	if _, err := store.DeleteGroup(ctx, edgeGroup.ID, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := store.GetNode(ctx, second.ID, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.GroupID != "" {
		t.Fatalf("deleted group left membership: %+v", afterDelete)
	}
}
