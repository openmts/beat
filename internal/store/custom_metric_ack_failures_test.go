package store

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestCustomMetricAcknowledgementRejectsInvalidAndUnprovenCompletions(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	tasks := NewAgentTaskStore(sqlite.DB)
	for _, completion := range []AgentTaskCompletion{
		{NodeID: "", TaskID: task.ID, Status: model.AgentTaskStatusDone},
		{NodeID: nodes[0], TaskID: task.ID, Status: model.AgentTaskStatusPending},
		{NodeID: nodes[1], TaskID: task.ID, Status: model.AgentTaskStatusDone},
	} {
		if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
			t.Fatal("invalid completion accepted")
		}
	}
	completion := AgentTaskCompletion{NodeID: nodes[0], TaskID: task.ID, Status: model.AgentTaskStatusFailed}
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("ordinary failed task treated as superseded")
	}
	if _, err := sqlite.DB.Exec("UPDATE agent_tasks SET completed_at=NULL WHERE id=?", task.ID); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("incomplete supersession evidence accepted")
	}
}

func TestCustomMetricAcknowledgementRequiresSupersessionAudit(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0]); err != nil {
		t.Fatal(err)
	}
	tasks := NewAgentTaskStore(sqlite.DB)
	completion := AgentTaskCompletion{NodeID: nodes[0], TaskID: task.ID, Status: model.AgentTaskStatusDone}
	installCustomMetricFault(t, sqlite.DB, "DELETE FROM admin_audit_events")
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("unaudited terminal state was acknowledged")
	}
	installCustomMetricFault(t, sqlite.DB, "ALTER TABLE admin_audit_events RENAME COLUMN action TO missing_action")
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("audit lookup failure was acknowledged")
	}
}

func TestCustomMetricAcknowledgementWriteFailuresPreservePendingTask(t *testing.T) {
	for _, failure := range []string{"update", "ignored update", "commit", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			sqlite, metrics, _, nodes := customRegistrationFixture(t)
			task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
			if err != nil {
				t.Fatal(err)
			}
			tasks := NewAgentTaskStore(sqlite.DB)
			ctx := t.Context()
			switch failure {
			case "update":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER custom_ack_fault BEFORE UPDATE ON agent_tasks BEGIN SELECT RAISE(ABORT,'write failed'); END")
			case "ignored update":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER custom_ack_fault BEFORE UPDATE ON agent_tasks BEGIN SELECT RAISE(IGNORE); END")
			case "commit":
				installCustomMetricCommitFault(t, sqlite.DB, "UPDATE ON agent_tasks")
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			completion := AgentTaskCompletion{NodeID: nodes[0], TaskID: task.ID, Status: model.AgentTaskStatusDone}
			if err := tasks.CompleteTaskForNode(ctx, completion); err == nil {
				t.Fatal("failed ACK write was accepted")
			}
			pending, err := tasks.GetPendingTasks(t.Context(), nodes[0])
			if err != nil || len(pending) != 1 || pending[0].ID != task.ID {
				t.Fatal("failed completion changed pending task")
			}
		})
	}
}

func TestCustomMetricAcknowledgementClosedStorage(t *testing.T) {
	sqlite := setupTestDB(t)
	tasks := NewAgentTaskStore(sqlite.DB)
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	err := tasks.CompleteTaskForNode(t.Context(), AgentTaskCompletion{NodeID: "node", TaskID: "task", Status: model.AgentTaskStatusDone})
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("closed store accepted completion")
	}
}
