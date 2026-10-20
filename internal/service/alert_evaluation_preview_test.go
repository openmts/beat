package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestEvaluationPreviewNeverMutatesStatesEventsOrOutbox(t *testing.T) {
	f := newEvaluationRuntimeFixture(t)
	f.report(t, 90, model.CollectedCustomSamples{})
	rule := alerteval.DefaultRule()
	rule.Name = "CPU"
	rule.Metric = "cpu"
	rule.Threshold = 80
	request := model.AlertEvaluationPreviewRequest{Rule: rule, NodeIDs: []string{f.node.ID}}
	preview, err := f.evaluator.Preview(t.Context(), "client", request)
	if err != nil || len(preview) != 1 || preview[0].Value == nil || *preview[0].Value != 90 || preview[0].SourceAt == nil {
		t.Fatal(preview, err)
	}
	for _, table := range []string{"alert_evaluation_states", "alert_events", "alert_evaluation_events", "notification_messages"} {
		var count int
		if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	request.NodeIDs = append(request.NodeIDs, f.node.ID)
	if _, err := f.evaluator.Preview(t.Context(), "client", request); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	request.NodeIDs = []string{"missing"}
	if _, err := f.evaluator.Preview(t.Context(), "client", request); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	request.NodeIDs = nil
	if _, err := f.evaluator.Preview(t.Context(), "client", request); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	request.NodeIDs = []string{f.node.ID}
	request.Rule.Name = ""
	if _, err := f.evaluator.Preview(t.Context(), "client", request); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.evaluator.previewNode(ctx, rule, f.node.ID); err == nil {
		t.Fatal("cancelled preview read ignored")
	}
	rule.Scope = model.AlertRuleScope{Kind: "nodes", NodeIDs: []string{"other"}}
	if preview, err := f.evaluator.previewNode(t.Context(), rule, f.node.ID); err != nil || preview.Status != model.EvaluationOutOfScope {
		t.Fatal(preview, err)
	}
}

func TestEvaluationPreviewAdmissionIsBoundedAndReleasesExactlyOnce(t *testing.T) {
	admission := evaluationPreviewAdmission{}
	if _, err := admission.acquire(""); !errors.Is(err, alerteval.ErrInvalidPolicy) {
		t.Fatal(err)
	}
	releases := []func(){}
	for index := range evaluationMaximumPreviews {
		release, err := admission.acquire(fmt.Sprint(index / 2))
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if _, err := admission.acquire("new"); !errors.Is(err, alerteval.ErrCapacity) {
		t.Fatal(err)
	}
	for _, release := range releases {
		release()
		release()
	}
	if admission.active != 0 || len(admission.clients) != 0 {
		t.Fatal(admission.active, admission.clients)
	}
}
