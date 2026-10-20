package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func evaluationRuleFixture(t *testing.T) (*SQLiteStore, *AlertEvaluationRuleStore, AlertRuleMutation) {
	t.Helper()
	sqlite := setupTestDB(t)
	owner := catalogPolicyWrite(t, NewMetricCatalogStore(sqlite.DB), model.MetricPolicyUpdate{})
	rule := alerteval.DefaultRule()
	rule.Name = "CPU"
	rule.Metric = "cpu"
	rule.Threshold = 80
	return sqlite, NewAlertEvaluationRuleStore(sqlite.DB), AlertRuleMutation{Rule: rule, Actor: EventMutationActor{UserID: owner.Audit.ActorID, SessionID: owner.SessionID}}
}

func TestAdvancedRuleRevisionScopeAndTombstonePreserveIncident(t *testing.T) {
	sqlite, rules, change := evaluationRuleFixture(t)
	node, err := NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "scope", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	change.Rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{node.ID}}
	created, err := rules.Create(t.Context(), change)
	if err != nil || created.Revision != 1 {
		t.Fatal(err)
	}
	change.Rule = *created
	change.ExpectedRevision = 1
	change.Rule.Name = "updated"
	updated, err := rules.Update(t.Context(), created.ID, change)
	if err != nil || updated.Revision != 2 || len(updated.Scope.NodeIDs) != 1 {
		t.Fatal("scope/revision not persisted", err)
	}
	if _, err := rules.Update(t.Context(), created.ID, change); !errors.Is(err, alerteval.ErrConflict) {
		t.Fatal("stale rule update overwrote policy", err)
	}
	if err := NewAlertEventStore(sqlite.DB).CreateEvent(t.Context(), &model.AlertEvent{ID: "incident", RuleID: created.ID, NodeID: node.ID, Status: model.AlertStatusAcknowledged, TriggeredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	change.ExpectedRevision = 2
	if err := rules.Delete(t.Context(), created.ID, change); err != nil {
		t.Fatal(err)
	}
	retained, err := rules.Get(t.Context(), created.ID)
	if err != nil || retained.DeletedAt == nil || retained.Enabled {
		t.Fatal("delete did not tombstone rule", err)
	}
	incident, err := NewAlertEventStore(sqlite.DB).GetActiveEvent(t.Context(), created.ID, node.ID)
	if err != nil || incident == nil || incident.Status != model.AlertStatusAcknowledged {
		t.Fatal("rule deletion erased active incident", err)
	}
}

func TestActiveIncidentRejectsNumericPolicyReplacementAndRechecksSession(t *testing.T) {
	sqlite, rules, change := evaluationRuleFixture(t)
	created, err := rules.Create(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewAlertEventStore(sqlite.DB).CreateEvent(t.Context(), &model.AlertEvent{ID: "incident", RuleID: created.ID, NodeID: "retained-node", Status: model.AlertStatusTriggered, TriggeredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	change.Rule = *created
	change.ExpectedRevision = 1
	change.Rule.Metric = "memory"
	if _, err := rules.Update(t.Context(), created.ID, change); !errors.Is(err, alerteval.ErrPolicyInUse) {
		t.Fatal("new metric could resolve old incident", err)
	}
	change.Rule = *created
	change.Rule.Enabled = false
	if _, err := rules.Update(t.Context(), created.ID, change); err != nil {
		t.Fatal("disable should preserve incident", err)
	}
	if err := NewAdminStore(sqlite.DB).RevokeSession(t.Context(), change.Actor.SessionID, time.Now()); err != nil {
		t.Fatal(err)
	}
	change.ExpectedRevision = 2
	if _, err := rules.Update(t.Context(), created.ID, change); !errors.Is(err, ErrAdminSessionNotFound) {
		t.Fatal("revoked actor changed rule", err)
	}
}
