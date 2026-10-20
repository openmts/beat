package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestValidateLegacyMetricBudgetBranches(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	base := legacyMetricRequest{metrics: []string{"cpu"}, from: now.Add(-time.Hour), to: now}
	cases := []struct {
		name     string
		request  legacyMetricRequest
		explicit bool
		want     error
	}{
		{"ok", base, true, nil},
		{"empty explicit", legacyMetricRequest{from: base.from, to: base.to}, true, metricquery.ErrInvalid},
		{"default empty", legacyMetricRequest{from: base.from, to: base.to}, false, metricquery.ErrInvalid},
		{"duplicate", legacyMetricRequest{metrics: []string{"cpu", "cpu"}, from: base.from, to: base.to}, true, metricquery.ErrInvalid},
		{"zero range", legacyMetricRequest{metrics: []string{"cpu"}, from: base.to, to: base.to}, true, metricquery.ErrInvalid},
		{"reversed", legacyMetricRequest{metrics: []string{"cpu"}, from: base.to, to: base.from}, true, metricquery.ErrInvalid},
		{"too wide", legacyMetricRequest{metrics: []string{"cpu"},
			from: now.Add(-metricquery.MaximumRange - time.Hour), to: now}, true, metricquery.ErrInvalid},
		{"future", legacyMetricRequest{metrics: []string{"cpu"},
			from: now.Add(time.Hour), to: now.Add(2 * time.Hour)}, true, metricquery.ErrInvalid},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := validateLegacyMetricBudget(test.request, test.explicit, now); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	tooMany := make([]string, 0, metricquery.MaximumSeries+1)
	for index := 0; index <= metricquery.MaximumSeries; index++ {
		tooMany = append(tooMany, "metric-"+string(rune('a'+index)))
	}
	if err := validateLegacyMetricBudget(legacyMetricRequest{metrics: tooMany, from: base.from, to: base.to},
		true, now); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("series budget error = %v", err)
	}
}

func TestAllowedLegacyMetricsBranches(t *testing.T) {
	allowed := map[string]bool{"cpu": true}
	if filtered, err := allowedLegacyMetrics([]string{"cpu", "memory"}, allowed, false); err != nil ||
		len(filtered) != 1 || filtered[0] != "cpu" {
		t.Fatalf("implicit filter = %v / %v", filtered, err)
	}
	if _, err := allowedLegacyMetrics([]string{"cpu", "memory"}, allowed, true); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("explicit error = %v", err)
	}
}

func TestLegacyMetricResponseMapsPoints(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	output := legacyMetricResponse("node-1", map[string][]store.TimePoint{
		"cpu": {{Timestamp: at, Value: 4.5}}})
	values, ok := output["cpu"]
	if !ok || len(values) != 1 || values[0].NodeID != "node-1" || values[0].Metric != "cpu" ||
		values[0].Value != 4.5 || values[0].Timestamp != at.Unix() {
		t.Fatalf("response = %+v", output)
	}
}

func TestParseLegacyMetricRequestCatalogBranches(t *testing.T) {
	database, err := store.NewSQLiteStore(handlerFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	nodes := store.NewNodeStore(database.DB)
	handler := NewNodeHandler(nodes, nil)
	handler.metricCatalog = store.NewMetricCatalogStore(database.DB)

	window := "?from=" + time.Now().UTC().Add(-time.Hour).Format(time.RFC3339) +
		"&to=" + time.Now().UTC().Format(time.RFC3339)
	unknown := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/metrics"+window+"&metric=not-a-key", nil)
	unknown.SetPathValue("id", "node-1")
	if _, err := handler.parseLegacyMetricRequest(unknown); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("unknown catalog key error = %v", err)
	}
	known := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/metrics"+window+"&metric=cpu", nil)
	known.SetPathValue("id", "node-1")
	if _, err := handler.parseLegacyMetricRequest(known); err != nil {
		t.Fatalf("catalog filter error = %v", err)
	}
	if _, err := handler.legacyAggregations(t.Context(), []string{"not-a-key"}); !errors.Is(err, metricquery.ErrInvalid) {
		t.Fatalf("missing definition error = %v", err)
	}
	if aggregations, err := handler.legacyAggregations(t.Context(), []string{"cpu"}); err != nil ||
		aggregations["cpu"] == "" {
		t.Fatalf("catalog aggregation = %v / %v", aggregations, err)
	}
	if _, err := handler.readLegacyMetrics(known, legacyMetricRequest{nodeID: "node-1",
		metrics: []string{"cpu"}, from: time.Now().Add(-time.Hour), to: time.Now()}); !errors.Is(err, metricquery.ErrUnavailable) {
		t.Fatalf("missing MTS error = %v", err)
	}
	_ = model.NowUTC
}

func TestHandleGetNodeMetricsNodeLookupFailure(t *testing.T) {
	database, mts := setupNodeTestDB(t)
	handler := NewNodeHandler(store.NewNodeStore(database.DB), mts)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/node-1/metrics", nil)
	request.SetPathValue("id", "node-1")
	response := httptest.NewRecorder()
	handler.HandleGetNodeMetrics(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}
