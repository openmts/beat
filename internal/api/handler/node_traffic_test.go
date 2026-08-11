package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestHandleUpdateNodeTrafficPolicy(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "traffic-policy", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	handler := NewNodeHandler(nodes, mtsStore)
	tests := []struct {
		name string
		body string
		want int
	}{
		{"negative limit", `{"traffic_limit":-1}`, http.StatusBadRequest},
		{"invalid type", `{"traffic_limit_type":"invalid"}`, http.StatusBadRequest},
		{"reset day zero", `{"traffic_reset_day":0}`, http.StatusBadRequest},
		{"reset day too large", `{"traffic_reset_day":32}`, http.StatusBadRequest},
		{
			"valid policy",
			`{"alias":"quota","group_id":"` + node.GroupID + `","traffic_limit":107374182400,"traffic_limit_type":"max","traffic_reset_day":31}`,
			http.StatusOK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/"+node.ID, strings.NewReader(test.body))
			request.SetPathValue("id", node.ID)
			response := httptest.NewRecorder()
			handler.HandleUpdateNode(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
	updated, err := nodes.GetNode(t.Context(), node.ID)
	if err != nil {
		t.Fatalf("get updated node: %v", err)
	}
	if updated.TrafficLimit != 107374182400 || updated.TrafficLimitType != model.TrafficLimitMax ||
		updated.TrafficResetDay != 31 {
		t.Fatalf("stored traffic policy = %#v", updated)
	}
}

func TestPublicNodeResponseIncludesMTSTrafficUsage(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "traffic-response", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	limit := int64(1000)
	limitType := model.TrafficLimitSum
	resetDay := 1
	if _, err := nodes.UpdateNode(t.Context(), node.ID, store.NodeUpdate{
		GroupID: node.GroupID, TrafficLimit: &limit,
		TrafficLimitType: &limitType, TrafficResetDay: &resetDay,
	}); err != nil {
		t.Fatalf("update traffic policy: %v", err)
	}
	now := time.Now().UTC()
	for index, totals := range []struct{ received, sent float64 }{{100, 200}, {160, 240}} {
		if err := mtsStore.WriteNodeMetrics(t.Context(), store.NodeMetricSample{
			NodeID:    node.ID,
			Metrics:   model.NodeMetrics{NetRecvTotal: totals.received, NetSentTotal: totals.sent},
			Timestamp: now.Add(time.Duration(index-2) * time.Minute),
		}); err != nil {
			t.Fatalf("write traffic sample: %v", err)
		}
	}
	if err := mtsStore.Flush(t.Context()); err != nil {
		t.Fatalf("flush traffic samples: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID, nil)
	request.SetPathValue("id", node.ID)
	response := httptest.NewRecorder()
	NewNodeHandler(nodes, mtsStore).HandleGetNode(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			Traffic model.TrafficSummary `json:"traffic"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode node response: %v", err)
	}
	if body.Data.Traffic.Received != 60 || body.Data.Traffic.Sent != 40 ||
		body.Data.Traffic.Used != 100 || body.Data.Traffic.Status != model.TrafficStatusNormal {
		t.Fatalf("traffic response = %#v", body.Data.Traffic)
	}
}
