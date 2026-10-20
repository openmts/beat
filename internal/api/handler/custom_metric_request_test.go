package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCustomMetricRequestsRejectUnknownTrailingAndOversizedJSON(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
	}{
		{"unknown field", `{"name":"queue","unexpected":true}`, http.StatusBadRequest},
		{"trailing object", `{} {}`, http.StatusBadRequest},
		{"trailing invalid", `{} garbage`, http.StatusBadRequest},
		{"missing body", "", http.StatusBadRequest},
		{"bad JSON", "{", http.StatusBadRequest},
		{"oversized value", `{"description":"` + strings.Repeat("x", maxCustomMetricRequestBytes) + `"}`, http.StatusRequestEntityTooLarge},
		{"oversized trailing whitespace", `{}` + strings.Repeat(" ", maxCustomMetricRequestBytes), http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, _, dispatcher, _ := setupCustomMetricHandler(t)
			response := httptest.NewRecorder()
			handler.HandleCreate(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body)))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if dispatcher.count() != 0 {
				t.Fatal("invalid request triggered dispatch")
			}
		})
	}
}

func TestCustomMetricUpdateAndBindingsRejectInvalidJSON(t *testing.T) {
	handler, _, _, sqlite := setupCustomMetricHandler(t)
	metric, _ := customMetricBoundToNode(t, handler, sqlite)
	for _, operation := range []http.HandlerFunc{handler.HandleUpdate, handler.HandleSetMetricNodes} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader("{"))
		request.SetPathValue("id", metric.ID)
		operation(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatal("invalid mutation JSON accepted")
		}
	}
}

func TestCustomMetricHandlerReportsStorageFailures(t *testing.T) {
	handler, _, _, sqlite := setupCustomMetricHandler(t)
	metric, _ := customMetricBoundToNode(t, handler, sqlite)
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []http.HandlerFunc{handler.HandleList, handler.HandleUpdate, handler.HandleListMetricNodes, handler.HandleSetMetricNodes} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"node_ids":[]}`))
		request.SetPathValue("id", metric.ID)
		response := httptest.NewRecorder()
		operation(response, request)
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("storage error reported as %d", response.Code)
		}
	}
}

func TestCustomMetricUpdateRejectsInvalidDefinitionAndCanDisable(t *testing.T) {
	handler, metrics, _, sqlite := setupCustomMetricHandler(t)
	metric, _ := customMetricBoundToNode(t, handler, sqlite)
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"name":"revoked_metric","shell_command":"","interval_seconds":5}`, http.StatusBadRequest},
		{`{"name":"revoked_metric","shell_command":"printf 2","interval_seconds":5,"enabled":false}`, http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(test.body))
		request.SetPathValue("id", metric.ID)
		response := httptest.NewRecorder()
		handler.HandleUpdate(response, request)
		if response.Code != test.status {
			t.Fatalf("update status=%d, want %d", response.Code, test.status)
		}
	}
	stored, err := metrics.Get(t.Context(), metric.ID)
	if err != nil || stored.Enabled {
		t.Fatal("disable was not persisted")
	}
}
