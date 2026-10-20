package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
)

func TestEvaluationStateAndHealthHistoryKeysetPagination(t *testing.T) {
	sqlite, states, change := evaluationCommitFixture(t)
	if err := states.Commit(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"a", "b"} {
		state := change.Decision.State
		state.NodeID += suffix
		values := evaluationStateValues(state)
		if _, err := sqlite.DB.Exec("INSERT INTO alert_evaluation_states("+evaluationStateColumns+") VALUES("+sqlPlaceholders(len(values))+")", values...); err != nil {
			t.Fatal(err)
		}
		id, epoch := uuid.NewString(), uuid.NewString()
		if _, err := sqlite.DB.Exec(`INSERT INTO alert_evaluation_events(id,rule_id,node_id,rule_revision,epoch_id,kind,occurred_at,source_key) VALUES(?,?,?,1,?,'no_data',?,?)`, id, state.RuleID, state.NodeID, epoch, time.Now().UnixMilli(), "source:"+id); err != nil {
			t.Fatal(err)
		}
	}
	query := AlertEvaluationQuery{Limit: 1, RuleID: change.Decision.State.RuleID, Phase: "firing", Status: "ok"}
	first, err := states.List(t.Context(), query)
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	query.Cursor = first.NextCursor
	second, err := states.List(t.Context(), query)
	if err != nil || len(second.Items) != 1 || second.Items[0].NodeID == first.Items[0].NodeID {
		t.Fatal(second, err)
	}
	eventQuery := AlertEvaluationQuery{RuleID: change.Decision.State.RuleID, Kind: "no_data", Limit: 1}
	events, err := states.ListEvents(t.Context(), eventQuery)
	if err != nil || len(events.Items) != 1 || events.NextCursor == "" {
		t.Fatal(events, err)
	}
	eventQuery.Cursor = events.NextCursor
	next, err := states.ListEvents(t.Context(), eventQuery)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID == events.Items[0].ID || next.NextCursor != "" {
		t.Fatal(next, err)
	}
}

func TestEvaluationListBoundsAndStorageErrors(t *testing.T) {
	_, states, _ := evaluationCommitFixture(t)
	for _, query := range []AlertEvaluationQuery{{Limit: -1}, {Limit: 201}, {Cursor: "bad!"}, {Cursor: "e30"}, {Phase: "bad"}, {Status: "bad"}} {
		if _, err := states.List(t.Context(), query); !errors.Is(err, alerteval.ErrInvalidPolicy) {
			t.Fatal(query, err)
		}
	}
	for _, query := range []AlertEvaluationQuery{{Limit: -1}, {Cursor: "bad!"}, {Cursor: "e30"}, {Kind: "bad"}} {
		if _, err := states.ListEvents(t.Context(), query); !errors.Is(err, alerteval.ErrInvalidPolicy) {
			t.Fatal(query, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := states.List(ctx, AlertEvaluationQuery{}); err == nil {
		t.Fatal("state read cancellation ignored")
	}
	if _, err := states.ListEvents(ctx, AlertEvaluationQuery{}); err == nil {
		t.Fatal("event read cancellation ignored")
	}
	if page, err := states.List(t.Context(), AlertEvaluationQuery{NodeID: "missing"}); err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	if page, err := states.ListEvents(t.Context(), AlertEvaluationQuery{NodeID: "missing"}); err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}

func TestAdvancedRuleListAndTransactionalCapacityGuard(t *testing.T) {
	sqlite, rules, change := evaluationRuleFixture(t)
	rule, err := rules.Create(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	list, err := rules.List(t.Context())
	if err != nil || len(list) != 1 || list[0].ID != rule.ID {
		t.Fatal(list, err)
	}
	if err := rules.Validate(t.Context(), *rule); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAlertPairCapacityTx(t.Context(), nil); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal(err)
	}
	tx, err := sqlite.DB.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := ValidateAlertPairCapacityTx(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rules.List(ctx); err == nil {
		t.Fatal("cancelled rule list ignored")
	}
}
