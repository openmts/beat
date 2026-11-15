package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTaskCenterLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

	tasks, err := store.ListTaskDefinitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("seeded task count = %d, want 0", len(tasks))
	}

	custom, err := store.CreateTask(ctx, "shell_script", "日志目录检查", "检查日志目录占用", "du -sh /var/log 2>/dev/null || true", json.RawMessage(`{}`), 60, 15, true, now)
	if err != nil {
		t.Fatal(err)
	}

	delivered, err := store.ScheduleAgentTaskRuns(ctx, "agent-1", "node-1", "edge-01", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 {
		t.Fatalf("delivered tasks = %d, want 1: %+v", len(delivered), delivered)
	}
	again, err := store.ScheduleAgentTaskRuns(ctx, "agent-1", "node-1", "edge-01", now.Add(10*time.Second))
	if err != nil || len(again) != 0 {
		t.Fatalf("duplicate schedule = %d, %v; want 0, nil", len(again), err)
	}

	var customRunID string
	for _, task := range delivered {
		if task.TaskID == custom.ID {
			customRunID = task.ID
			if task.Script != custom.Script || task.Kind != "shell_script" || task.TimeoutSeconds != 15 {
				t.Fatalf("unexpected custom task payload: %+v", task)
			}
		}
	}
	if customRunID == "" {
		t.Fatal("custom task run was not delivered")
	}
	completed, err := store.CompleteTaskRun(ctx, customRunID, "agent-1", "success", "12M\t/var/log", 0, 42, now.Add(20*time.Second))
	if err != nil || !completed {
		t.Fatalf("CompleteTaskRun() = %v, %v", completed, err)
	}
	runs, err := store.ListTaskRuns(ctx, custom.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].Output != "12M\t/var/log" || runs[0].NodeName != "edge-01" {
		t.Fatalf("unexpected task runs: %+v", runs)
	}
	detail, err := store.GetTaskRun(ctx, customRunID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DurationMs != 42 || detail.ExitCode != 0 || !strings.Contains(detail.Script, "du -sh") {
		t.Fatalf("unexpected run detail: %+v", detail)
	}

	if err := store.DeleteCustomTask(ctx, custom.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetTaskRun(ctx, customRunID); err != nil {
		t.Fatalf("run history was removed with custom task: %v", err)
	}
}

func TestNetworkProbeTaskConfigLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	config := json.RawMessage(`{"targets":[{"carrier":"telecom","label":"电信","host":"127.0.0.1","port":9099,"protocol":"tcp","addressFamily":"auto"},{"carrier":"unicom","label":"联通","host":"127.0.0.1","port":9099,"protocol":"tcp","addressFamily":"auto"},{"carrier":"mobile","label":"移动","host":"127.0.0.1","port":9099,"protocol":"tcp","addressFamily":"auto"}],"probe":{"count":3,"intervalMs":50,"timeoutMs":500}}`)
	task, err := store.CreateTask(ctx, "three_network_probe", "三网质量", "TCP 探测", "", config, 60, 60, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != "three_network_probe" || !strings.Contains(string(task.Config), `"telecom"`) {
		t.Fatalf("unexpected network task: %+v", task)
	}
	delivered, err := store.ScheduleAgentTaskRuns(ctx, "agent-network", "node-network", "edge-network", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered[0].Kind != "three_network_probe" || delivered[0].Script != "" || !strings.Contains(string(delivered[0].Config), `"mobile"`) {
		t.Fatalf("unexpected network task dispatch: %+v", delivered)
	}
}
