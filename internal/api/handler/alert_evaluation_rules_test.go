package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
)

func TestEvaluationRuleHTTPRevisionConflictAndTombstone(t *testing.T) {
	f := newEvaluationHTTPFixture(t)
	write := evaluationRuleWrite{AlertEvaluationRule: f.rule, ExpectedRevision: 1}
	write.Name = "new name"
	response := f.call(t, evaluationHTTPCall{Handle: f.handler.HandleSaveEvaluationRule, Method: "PUT", Body: evaluationJSON(t, write), Item: true})
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleSaveEvaluationRule, Method: "PUT", Body: evaluationJSON(t, write), Item: true})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "stale_revision") {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, handle := range []http.HandlerFunc{f.handler.HandleListEvaluationRules, f.handler.HandleGetEvaluationRule} {
		response = f.call(t, evaluationHTTPCall{Handle: handle, Method: "GET", Item: true})
		if response.Code != http.StatusOK {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleDeleteEvaluationRule, Method: "DELETE", Body: `{"expected_revision":2}`, Item: true})
	if response.Code != http.StatusNoContent {
		t.Fatal(response.Code, response.Body.String())
	}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleGetEvaluationRule, Method: "GET", Item: true})
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandleSaveEvaluationRule, Method: "POST", Body: evaluationJSON(t, write)})
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestEvaluationHTTPRejectsUnauthenticatedAndMalformedWrites(t *testing.T) {
	f := newEvaluationHTTPFixture(t)
	principal := f.principal
	f.principal = model.AdminPrincipal{}
	for _, handle := range []http.HandlerFunc{f.handler.HandleListEvaluationRules, f.handler.HandleGetEvaluationRule, f.handler.HandleSaveEvaluationRule, f.handler.HandleDeleteEvaluationRule, f.handler.HandlePreviewEvaluationRule, f.handler.HandleListEvaluations, f.handler.HandleListEvaluationEvents, f.handler.HandleGetEvaluation} {
		response := f.call(t, evaluationHTTPCall{Handle: handle, Method: "POST", Body: "{}", Item: true})
		if response.Code != http.StatusUnauthorized {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	f.principal = principal
	for _, body := range []string{"broken", "{}", `{"unknown":1}`, strings.Repeat(" ", alerteval.MaximumRequestBytes+1)} {
		for _, handle := range []http.HandlerFunc{f.handler.HandleSaveEvaluationRule, f.handler.HandleDeleteEvaluationRule} {
			response := f.call(t, evaluationHTTPCall{Handle: handle, Method: "PUT", Body: body, Item: true})
			if response.Code != http.StatusBadRequest {
				t.Fatal(response.Code, response.Body.String())
			}
		}
	}
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP"); err != nil {
		t.Fatal(err)
	}
	response := f.call(t, evaluationHTTPCall{Handle: f.handler.HandleSaveEvaluationRule, Method: "POST", Body: evaluationJSON(t, f.rule)})
	if response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestEvaluationPreviewHTTPKeepsNullAndReturnsCapacity(t *testing.T) {
	f := newEvaluationHTTPFixture(t)
	body := evaluationJSON(t, model.AlertEvaluationPreviewRequest{Rule: f.rule, NodeIDs: []string{f.node.ID}})
	response := f.call(t, evaluationHTTPCall{Handle: f.handler.HandlePreviewEvaluationRule, Method: "POST", Body: body})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code)
	}
	f.handler.config.Preview = evaluationPreviewFake{}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandlePreviewEvaluationRule, Method: "POST", Body: body})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"value":null`) {
		t.Fatal(response.Code, response.Body.String())
	}
	f.handler.config.Preview = evaluationPreviewFake{err: alerteval.ErrCapacity}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandlePreviewEvaluationRule, Method: "POST", Body: body})
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "30" {
		t.Fatal(response.Code)
	}
	response = f.call(t, evaluationHTTPCall{Handle: f.handler.HandlePreviewEvaluationRule, Method: "POST", Body: "bad"})
	if response.Code != http.StatusBadRequest {
		t.Fatal(response.Code)
	}
}
