package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestCleanupRemovesExpiredOperationalData(t *testing.T) {
	store, err := Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := now.Add(-100 * 24 * time.Hour)
	if err := store.CreateAdminSession(ctx, "expired-session", "admin", 1, "csrf", old, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAdminSession(ctx, "active-session", "admin", 1, "csrf", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAudit(ctx, AuditEntry{Event: "old", CreatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAudit(ctx, AuditEntry{Event: "new", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO task_runs (id, task_id, task_name, task_source, agent_id, node_id, script, timeout_seconds, status, scheduled_for, scheduled_at, created_at) VALUES ('old-run', 'task', 'task', 'custom', 'agent', 'node', '', 10, 'success', ?, ?, ?)`, old.Unix(), old.Unix(), old.Unix()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO agent_tasks (id, agent_id, node_id, kind, status, created_at) VALUES ('old-agent-task', 'agent', 'node', 'shell_script', 'success', ?)`,
		`INSERT INTO agent_tombstones (agent_id, node_id, created_at) VALUES ('agent', 'node', ?)`,
	} {
		if _, err := store.db.ExecContext(ctx, query, old.Unix()); err != nil {
			t.Fatal(err)
		}
	}

	result, err := store.Cleanup(ctx, now, 30*24*time.Hour, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.AdminSessions != 1 || result.TaskRuns != 1 || result.AgentTasks != 1 || result.AgentTombstones != 1 || result.AuditEntries != 1 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}
	if _, ok, err := store.GetAdminSession(ctx, "active-session"); err != nil || !ok {
		t.Fatalf("active session was removed: ok=%v err=%v", ok, err)
	}
}
