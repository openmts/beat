package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type evaluationHTTPFixture struct {
	db        *store.SQLiteStore
	handler   *AlertEvaluationHandler
	principal model.AdminPrincipal
	rule      model.AlertEvaluationRule
	node      model.Node
}

func newEvaluationHTTPFixture(t *testing.T) *evaluationHTTPFixture {
	t.Helper()
	db := setupTestDB(t)
	principal := dashboardHandlerPrincipal(t, store.NewAdminStore(db.DB))
	node, err := store.NewNodeStore(db.DB).UpsertNode(t.Context(), "api-evaluation", "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	rules := store.NewAlertEvaluationRuleStore(db.DB)
	rule := alerteval.DefaultRule()
	rule.Name = "CPU"
	rule.Metric = "cpu"
	rule.Threshold = 80
	created, err := rules.Create(t.Context(), store.AlertRuleMutation{Rule: rule, Actor: store.EventMutationActor{UserID: principal.User.ID, SessionID: principal.Session.ID}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewAlertEvaluationHandler(AlertEvaluationHandlerConfig{Rules: rules, States: store.NewAlertEvaluationStore(db.DB), Receipts: evaluationReceiptFake{err: notificationqueue.ErrNotFound}})
	return &evaluationHTTPFixture{db: db, handler: h, principal: principal, rule: *created, node: *node}
}

type evaluationHTTPCall struct {
	Handle              http.HandlerFunc
	Method, Body, Query string
	Item                bool
}

func (f *evaluationHTTPFixture) call(t *testing.T, input evaluationHTTPCall) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(input.Method, "http://beat.test/api/v1/alerts/rules"+input.Query, strings.NewReader(input.Body))
	if input.Item {
		request.SetPathValue("id", f.rule.ID)
	}
	request.SetPathValue("rule_id", f.rule.ID)
	request.SetPathValue("node_id", f.node.ID)
	if f.principal.User.ID != "" {
		request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), f.principal))
	}
	response := httptest.NewRecorder()
	input.Handle(response, request)
	return response
}

func evaluationJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

type evaluationReceiptFake struct{ err error }

func (f evaluationReceiptFake) SourceReceipt(context.Context, string) (notificationqueue.Receipt, error) {
	return notificationqueue.Receipt{State: "not_routed"}, f.err
}

type evaluationPreviewFake struct{ err error }

func (f evaluationPreviewFake) Preview(context.Context, string, model.AlertEvaluationPreviewRequest) ([]model.AlertEvaluationPreview, error) {
	return []model.AlertEvaluationPreview{{NodeID: "node", Status: model.EvaluationNoData}}, f.err
}
