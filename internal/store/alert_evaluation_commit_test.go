package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func evaluationCommitFixture(t *testing.T) (*SQLiteStore, *AlertEvaluationStore, AlertEvaluationCommit) {
	t.Helper()
	sqlite, rules, mutation := evaluationRuleFixture(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "evaluation-node", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := rules.Create(t.Context(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	value := 90.0
	now := time.Now().UTC()
	decision, err := alerteval.Step(alerteval.StepInput{Rule: *rule, Node: *node, Observation: model.AlertEvaluationObservation{ObservedAt: now, SourceAt: now, RunID: "run", EvidenceID: "sample", Cadence: time.Second, Status: model.EvaluationOK, Value: &value}})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := NewMetricCatalogStore(sqlite.DB).Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	return sqlite, NewAlertEvaluationStore(sqlite.DB), AlertEvaluationCommit{Decision: decision, ExpectedStateVersion: 0, RuleRevision: rule.Revision, Policies: []AlertMetricPolicyReference{{Key: "cpu", Revision: definition.Revision}}}
}

func TestEvaluationStateAndIncidentCommitUseCASAndRetainIdentity(t *testing.T) {
	sqlite, states, change := evaluationCommitFixture(t)
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	stored, err := states.Get(t.Context(), AlertEvaluationKey{RuleID: change.Decision.State.RuleID, NodeID: change.Decision.State.NodeID})
	if err != nil || stored == nil || stored.Version != 1 || stored.ActiveEventID == "" {
		t.Fatal("state and incident did not commit", err)
	}
	if err := states.Commit(t.Context(), change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal("duplicate evaluation bypassed state CAS", err)
	}
	var incidents int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM alert_events").Scan(&incidents); err != nil || incidents != 1 {
		t.Fatal("duplicate incident", err)
	}
}

func TestEvaluationCommitRechecksRulePolicyScopeAndRollsBackFailures(t *testing.T) {
	for _, scenario := range []string{"rule disabled", "rule revision", "metric disabled", "metric revision", "node removed", "audit failure"} {
		t.Run(scenario, func(t *testing.T) {
			sqlite, states, change := evaluationCommitFixture(t)
			statements := map[string]string{
				"rule disabled":   "UPDATE alert_rules SET enabled=0",
				"rule revision":   "UPDATE alert_rules SET revision=revision+1",
				"metric disabled": "UPDATE metric_definitions SET enabled=0 WHERE metric_key='cpu'",
				"metric revision": "UPDATE metric_definitions SET revision=revision+1 WHERE metric_key='cpu'",
				"node removed":    "DELETE FROM nodes",
				"audit failure":   `CREATE TRIGGER reject_evaluation_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit failed'); END`,
			}
			if _, err := sqlite.DB.Exec(statements[scenario]); err != nil {
				t.Fatal(err)
			}
			if err := states.Commit(t.Context(), change); err == nil {
				t.Fatal("stale/failing evaluation committed")
			}
			for _, table := range []string{"alert_evaluation_states", "alert_events"} {
				var count int
				if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("failed evaluation leaked state", table, err)
				}
			}
		})
	}
}

func TestEvaluationProgressRejectsConcurrentAcknowledgement(t *testing.T) {
	sqlite, states, change := evaluationCommitFixture(t)
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	state := change.Decision.State
	change.ExpectedStateVersion = state.Version
	change.Decision.State.Version++
	change.Decision.Actions = nil
	if _, err := sqlite.DB.Exec("UPDATE alert_events SET status='acknowledged' WHERE id=?", state.ActiveEventID); err != nil {
		t.Fatal(err)
	}
	if err := states.Commit(t.Context(), change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal("concurrent ACK overwritten", err)
	}
	change.Decision.State.ActiveEventStatus = model.AlertStatusAcknowledged
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal("fresh ACK evidence rejected", err)
	}
}
