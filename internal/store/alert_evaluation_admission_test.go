package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
)

func TestEvaluationAdmissionRollsBackCapacityAndStorageFailure(t *testing.T) {
	for _, scenario := range []string{"state capacity", "rule storage", "state storage", "state layout", "insert failure"} {
		t.Run(scenario, func(t *testing.T) {
			sqlite, states, _ := evaluationCommitFixture(t)
			statements := map[string]string{
				"state capacity": `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<100000)
     INSERT INTO alert_evaluation_states(rule_id,node_id,rule_revision,state_version,phase,evaluation_status,updated_at)
     SELECT 'retired',CAST(n AS TEXT),1,1,'normal','disabled',0 FROM numbers`,
				"rule storage":   "DROP TABLE alert_rules",
				"state storage":  "DROP TABLE alert_evaluation_states",
				"state layout":   "DROP TABLE alert_evaluation_states; CREATE TABLE alert_evaluation_states(unrelated INTEGER)",
				"insert failure": `CREATE TRIGGER reject_admission BEFORE INSERT ON alert_evaluation_states BEGIN SELECT RAISE(ABORT,'fixture'); END`,
			}
			if _, err := sqlite.DB.Exec(statements[scenario]); err != nil {
				t.Fatal(err)
			}
			_, err := states.Admit(t.Context(), time.Now())
			if err == nil {
				t.Fatal("failed admission accepted")
			}
			if scenario == "state capacity" && !errors.Is(err, alerteval.ErrCapacity) {
				t.Fatal(err)
			}
		})
	}
}

func TestEvaluationIncidentRecheckRecoveryAndStorageFailure(t *testing.T) {
	sqlite, states, change := evaluationCommitFixture(t)
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	event := change.Decision.Actions[0].Event
	change.ExpectedStateVersion = 1
	change.Decision.State.Version = 2
	change.Decision.State.ActiveEventID = ""
	change.Decision.State.ActiveEventStatus = ""
	change.Decision.Actions = []alerteval.Action{{Kind: alerteval.ActionThresholdRecovery, Event: event}}
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := recheckEvaluationIncident(t.Context(), tx, change); err != nil {
		t.Fatal(err)
	}
	change.Decision.Actions[0].Event = nil
	if err := recheckEvaluationIncident(t.Context(), tx, change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := tx.Exec("DROP TABLE alert_events"); err != nil {
		t.Fatal(err)
	}
	change.Decision.Actions = nil
	if err := recheckEvaluationIncident(t.Context(), tx, change); err == nil {
		t.Fatal("storage failure ignored")
	}
}
