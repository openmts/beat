package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestPublicTelemetryHandlerReadsAndUpdatesPolicy(t *testing.T) {
	sqliteStore, _ := setupNodeTestDB(t)
	handler := NewPublicTelemetryHandler(store.NewPublicTelemetryStore(sqliteStore.DB))
	get := httptest.NewRecorder()
	handler.HandleGet(get, jsonRequest(http.MethodGet, "/api/v1/admin/settings/public-telemetry", nil))
	if expected, actual := http.StatusOK, get.Code; expected != actual {
		t.Fatalf("get status = %d, want %d", actual, expected)
	}
	policy, err := decodeTelemetryPolicy(get.Body.Bytes())
	if err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	if policy.Enabled || policy.Revision != 1 {
		t.Fatalf("policy = %#v", policy)
	}

	policy.Enabled = true
	policy.Revision = 2
	policy.AllowedEvents = []string{model.PublicInteractionPageView}
	body, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("encode policy: %v", err)
	}
	update := httptest.NewRecorder()
	handler.HandleUpdate(update, jsonRequest(http.MethodPut, "/api/v1/admin/settings/public-telemetry", strings.NewReader(string(body))))
	if expected, actual := http.StatusOK, update.Code; expected != actual {
		t.Fatalf("update status = %d, want %d (%s)", actual, expected, update.Body.String())
	}
	updated, err := decodeTelemetryPolicy(update.Body.Bytes())
	if err != nil {
		t.Fatalf("decode updated policy: %v", err)
	}
	if !updated.Enabled || updated.Revision != 2 || updated.UpdatedAt.IsZero() {
		t.Fatalf("updated policy = %#v", updated)
	}
}

func decodeTelemetryPolicy(body []byte) (model.PublicTelemetryPolicy, error) {
	var envelope struct {
		Data model.PublicTelemetryPolicy `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return model.PublicTelemetryPolicy{}, err
	}
	return envelope.Data, nil
}

func TestPublicTelemetryHandlerRejectsInvalidRequests(t *testing.T) {
	sqliteStore, _ := setupNodeTestDB(t)
	handler := NewPublicTelemetryHandler(store.NewPublicTelemetryStore(sqliteStore.DB))
	cases := []struct {
		name string
		body string
		want int
	}{
		{name: "malformed", body: `{`, want: http.StatusBadRequest},
		{name: "invalid enum", body: `{"enabled":true,"allowed_events":["click"],"sample_ceiling":10,"retention_days":30,"revision":2}`,
			want: http.StatusBadRequest},
		{name: "stale revision", body: `{"enabled":true,"allowed_events":["page_view"],"sample_ceiling":10,"retention_days":30,"revision":9}`,
			want: http.StatusConflict},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.HandleUpdate(recorder, jsonRequest(http.MethodPut, "/api/v1/admin/settings/public-telemetry", strings.NewReader(testCase.body)))
			if expected, actual := testCase.want, recorder.Code; expected != actual {
				t.Fatalf("status = %d, want %d (%s)", actual, expected, recorder.Body.String())
			}
		})
	}
}

func TestPublicTelemetryHandlerWithoutStore(t *testing.T) {
	handler := NewPublicTelemetryHandler(nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		recorder := httptest.NewRecorder()
		handler.HandleGet(recorder, jsonRequest(method, "/api/v1/admin/settings/public-telemetry", nil))
		if method == http.MethodPut {
			handler.HandleUpdate(recorder, jsonRequest(method, "/api/v1/admin/settings/public-telemetry", strings.NewReader(`{}`)))
		}
		if expected, actual := http.StatusServiceUnavailable, recorder.Code; expected != actual {
			t.Fatalf("%s status = %d, want %d", method, actual, expected)
		}
	}
}

func TestSiteSettingsHandlerReportsPublicTelemetry(t *testing.T) {
	sqliteStore, _ := setupNodeTestDB(t)
	defaultResponse := httptest.NewRecorder()
	NewSiteSettingsHandler(store.NewSiteSettingsStore(sqliteStore.DB)).HandleGet(defaultResponse,
		jsonRequest(http.MethodGet, "/api/v1/settings/site", nil))
	if !strings.Contains(defaultResponse.Body.String(), `"public_telemetry":{"enabled":false,"allowed_events":[],"sample_ceiling":0}`) {
		t.Fatalf("default body = %s", defaultResponse.Body.String())
	}

	policy := enabledPolicy()
	policy.AllowedEvents = []string{model.PublicInteractionPageView}
	response := httptest.NewRecorder()
	NewSiteSettingsHandler(store.NewSiteSettingsStore(sqliteStore.DB)).
		WithPublicTelemetry(&fakePublicTelemetryPolicy{policy: policy}).
		HandleGet(response, jsonRequest(http.MethodGet, "/api/v1/settings/site", nil))
	var envelope struct {
		Data struct {
			PublicTelemetry publicTelemetryView `json:"public_telemetry"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if !envelope.Data.PublicTelemetry.Enabled || len(envelope.Data.PublicTelemetry.AllowedEvents) != 1 ||
		envelope.Data.PublicTelemetry.SampleCeiling != policy.SampleCeiling {
		t.Fatalf("public telemetry = %#v", envelope.Data.PublicTelemetry)
	}
	if strings.Contains(response.Body.String(), "updated_by") || strings.Contains(response.Body.String(), "revision") {
		t.Fatalf("public settings must not expose policy metadata: %s", response.Body.String())
	}

	failing := httptest.NewRecorder()
	NewSiteSettingsHandler(store.NewSiteSettingsStore(sqliteStore.DB)).
		WithPublicTelemetry(&fakePublicTelemetryPolicy{err: errors.New("unavailable")}).
		HandleGet(failing, jsonRequest(http.MethodGet, "/api/v1/settings/site", nil))
	if !strings.Contains(failing.Body.String(), `"public_telemetry":{"enabled":false,"allowed_events":[],"sample_ceiling":0}`) {
		t.Fatalf("failing body = %s", failing.Body.String())
	}
}
