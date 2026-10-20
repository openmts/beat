package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMetricDashboardDefaultIsAvailableWithoutLogin(t *testing.T) {
	router := setupSessionRouter(t)
	response := requestRouter(router, http.MethodGet, "/api/v1/metrics/dashboards/default", "", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("public default dashboard status = %d", response.Code)
	}
	var payload struct {
		Data struct {
			Definition struct {
				Version int               `json:"version"`
				Panels  []json.RawMessage `json:"panels"`
			} `json:"definition"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Definition.Version != 1 || len(payload.Data.Definition.Panels) == 0 {
		t.Fatal("public default dashboard has no usable metric panels")
	}
}
