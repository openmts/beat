package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/store"
)

func TestAlertEvaluationRoutesRequireExplicitProtection(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAlertEvaluationRoutes(mux, AlertEvaluationRouteConfig{})
	request := httptest.NewRequest("GET", "/api/v1/alerts/evaluations", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code)
	}
	RegisterAlertEvaluationRoutes(nil, AlertEvaluationRouteConfig{})
	wrapped := 0
	RegisterAlertEvaluationRoutes(mux, AlertEvaluationRouteConfig{Handler: handler.AlertEvaluationHandlerConfig{Rules: store.NewAlertEvaluationRuleStore(nil), States: store.NewAlertEvaluationStore(nil)}, ProtectAdmin: func(http.Handler) http.Handler {
		wrapped++
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	}})
	if wrapped != 9 {
		t.Fatal("missing protected routes", wrapped)
	}
	for _, route := range []struct{ method, path string }{{"GET", "/rules"}, {"POST", "/rules"}, {"GET", "/rules/rule"}, {"PUT", "/rules/rule"}, {"DELETE", "/rules/rule"}, {"POST", "/rules/preview"}, {"GET", "/evaluations"}, {"GET", "/evaluations/rule/node"}, {"GET", "/evaluation-events"}} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(route.method, "/api/v1/alerts"+route.path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatal(route, response.Code)
		}
	}
}
