package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

func TestEvaluationReadHTTPShowsUnknownHistoryWithoutFabricatingDelivery(t *testing.T) {
	f := newEvaluationHTTPFixture(t)
	response := f.call(t, evaluationHTTPCall{Handle: f.handler.HandleGetEvaluation, Method: "GET"})
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code)
	}
	at := time.Now()
	if _, err := f.handler.config.States.Admit(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE alert_evaluation_states SET active_event_id='incident',active_event_status='acknowledged',phase='firing',last_evidence_at_ns=?", at.UnixNano()); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err := f.db.DB.Exec(`INSERT INTO alert_evaluation_events(id,rule_id,node_id,rule_revision,epoch_id,kind,occurred_at,notified_at,source_key) VALUES(?,?,?,1,?,'no_data',?,?,'health-source')`, id, f.rule.ID, f.node.ID, uuid.NewString(), at.UnixMilli(), at.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for _, handle := range []http.HandlerFunc{f.handler.HandleListEvaluations, f.handler.HandleListEvaluationEvents, f.handler.HandleGetEvaluation} {
		response = f.call(t, evaluationHTTPCall{Handle: handle, Method: "GET"})
		if response.Code != http.StatusOK {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), "history_unavailable") || !strings.Contains(response.Body.String(), `"window":{"from":`) {
		t.Fatal(response.Body.String())
	}
	f.handler.config.Receipts = evaluationReceiptFake{}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleGetEvaluation, Method: "GET"})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "not_routed") {
		t.Fatal(response.Code, response.Body.String())
	}
	f.handler.config.Receipts = evaluationReceiptFake{err: notificationqueue.ErrQueueUnavailable}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleGetEvaluation, Method: "GET"})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code)
	}
	f.handler.config.Receipts = nil
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleGetEvaluation, Method: "GET"})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code)
	}
}

func TestEvaluationReadHTTPBoundsAndDatabaseFailures(t *testing.T) {
	f := newEvaluationHTTPFixture(t)
	for _, query := range []string{"?limit=bad", "?limit=201", "?cursor=invalid!", "?node_id=" + strings.Repeat("x", 129)} {
		for _, handle := range []http.HandlerFunc{f.handler.HandleListEvaluations, f.handler.HandleListEvaluationEvents} {
			response := f.call(t, evaluationHTTPCall{Handle: handle, Method: "GET", Query: query})
			if response.Code != http.StatusBadRequest {
				t.Fatal(response.Code, response.Body.String())
			}
		}
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, handle := range []http.HandlerFunc{f.handler.HandleListEvaluationRules, f.handler.HandleGetEvaluationRule, f.handler.HandleListEvaluations, f.handler.HandleListEvaluationEvents, f.handler.HandleGetEvaluation} {
		response := f.call(t, evaluationHTTPCall{Handle: handle, Method: "GET", Item: true})
		if response.Code != http.StatusServiceUnavailable {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	response := f.call(t, evaluationHTTPCall{Handle: f.handler.HandleDeleteEvaluationRule, Method: "DELETE", Item: true, Body: `{"expected_revision":1}`})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code)
	}
}

func TestEvaluationHTTPErrorsAndStrictPayload(t *testing.T) {
	for _, err := range []error{alerteval.ErrPolicyInUse, store.ErrAlertEvaluationRuleNotFound, alerteval.ErrUnavailable} {
		response := httptest.NewRecorder()
		evaluationError(response, err)
		if response.Code < 400 {
			t.Fatal(response.Code)
		}
	}
	request := httptest.NewRequest("POST", "/", strings.NewReader("{} {}"))
	if err := decodeEvaluationBody(request, new(evaluationRuleWrite)); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
}
