package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestSupersededCustomMetricAcknowledgementIsOwnedAndIdempotent(t *testing.T) {
	sqlite, metrics, _, nodes := customRegistrationFixture(t)
	first, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	latest, err := metrics.QueueNodeCustomMetrics(t.Context(), nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	tasks := NewAgentTaskStore(sqlite.DB)
	before := captureCustomMetricACKState(t, tasks, first.ID)
	completion := AgentTaskCompletion{NodeID: nodes[0], TaskID: first.ID, Status: model.AgentTaskStatusDone}
	for _, status := range []string{model.AgentTaskStatusDone, model.AgentTaskStatusFailed, model.AgentTaskStatusDone} {
		completion.Status = status
		if err := tasks.CompleteTaskForNode(t.Context(), completion); err != nil {
			t.Fatal(err)
		}
	}
	if after := captureCustomMetricACKState(t, tasks, first.ID); after != before || after.status != model.AgentTaskStatusFailed || after.auditCount != 1 {
		t.Fatal("duplicate ACK changed supersession evidence")
	}
	completion.NodeID = nodes[1]
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("cross-node acknowledgement accepted")
	}
	completion.NodeID = nodes[0]
	completion.TaskID = "unknown"
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("unknown task acknowledged")
	}
	completion.TaskID = latest.ID
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CompleteTaskForNode(t.Context(), completion); err == nil {
		t.Fatal("ordinary completed task mistaken for superseded registration")
	}
}

type customMetricACKState struct {
	status, created, attempted, completed, payload, audits string
	auditCount                                             int
}

func captureCustomMetricACKState(t *testing.T, tasks *AgentTaskStore, id string) customMetricACKState {
	t.Helper()
	var state customMetricACKState
	err := tasks.db.QueryRow(`SELECT status,CAST(created_at AS TEXT),COALESCE(CAST(attempted_at AS TEXT),''),
		CAST(completed_at AS TEXT),payload FROM agent_tasks WHERE id=?`, id).
		Scan(&state.status, &state.created, &state.attempted, &state.completed, &state.payload)
	if err != nil {
		t.Fatal(err)
	}
	err = tasks.db.QueryRow(`SELECT COUNT(*),json_group_array(json_object('id',id,'detail',detail_json,'created_at',created_at))
		FROM admin_audit_events WHERE action='agent.custom_metrics.supersede'`).Scan(&state.auditCount, &state.audits)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
