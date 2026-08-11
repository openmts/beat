package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/store"
)

func TestPublicNodeHandlersExcludeHiddenNodes(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "hidden-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	isPublic := false
	privateRemark := "do not expose"
	if _, err := nodes.UpdateNode(t.Context(), node.ID, store.NodeUpdate{
		Alias: node.Alias, GroupID: node.GroupID, SSHPublicKey: node.SSHPublicKey,
		IsPublic: &isPublic, PrivateRemark: &privateRemark,
	}); err != nil {
		t.Fatalf("hide node: %v", err)
	}
	handler := NewNodeHandler(nodes, mtsStore)

	list := httptest.NewRecorder()
	handler.HandleListNodes(list, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "hidden-node") ||
		strings.Contains(list.Body.String(), privateRemark) {
		t.Fatalf("public list status = %d, body = %s", list.Code, list.Body.String())
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID, nil)
	detailRequest.SetPathValue("id", node.ID)
	detail := httptest.NewRecorder()
	handler.HandleGetNode(detail, detailRequest)
	if detail.Code != http.StatusNotFound {
		t.Fatalf("hidden detail status = %d, body = %s", detail.Code, detail.Body.String())
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics?from="+
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)+"&to="+
		time.Now().UTC().Format(time.RFC3339), nil)
	metricsRequest.SetPathValue("id", node.ID)
	metrics := httptest.NewRecorder()
	handler.HandleGetNodeMetrics(metrics, metricsRequest)
	if metrics.Code != http.StatusNotFound {
		t.Fatalf("hidden metrics status = %d, body = %s", metrics.Code, metrics.Body.String())
	}

	managed := managedNodeRequestForTest(
		handler.HandleListManagedNodes, http.MethodGet, "/api/v1/nodes/manage", "", "",
	)
	if managed.Code != http.StatusOK || !strings.Contains(managed.Body.String(), privateRemark) ||
		!strings.Contains(managed.Body.String(), `"is_public":false`) {
		t.Fatalf("managed nodes status = %d, body = %s", managed.Code, managed.Body.String())
	}
}

func TestPublicNodeHandlersRespectIPVisibility(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "public-node", "2001:db8::1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	settingsStore := store.NewSiteSettingsStore(sqliteStore.DB)
	settings, err := settingsStore.Get(t.Context())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	settings.ShowIPAddresses = false
	if _, err := settingsStore.Update(t.Context(), settings); err != nil {
		t.Fatalf("hide IP addresses: %v", err)
	}
	handler := NewNodeHandler(nodes, mtsStore, settingsStore)

	list := httptest.NewRecorder()
	handler.HandleListNodes(list, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "2001:db8::1") {
		t.Fatalf("public list status = %d, body = %s", list.Code, list.Body.String())
	}
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+node.ID, nil)
	detailRequest.SetPathValue("id", node.ID)
	detail := httptest.NewRecorder()
	handler.HandleGetNode(detail, detailRequest)
	var response Response
	if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &response) != nil {
		t.Fatalf("public detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	if strings.Contains(detail.Body.String(), "2001:db8::1") || !strings.Contains(detail.Body.String(), `"host":""`) {
		t.Fatalf("public detail body = %s", detail.Body.String())
	}
}

func TestHandleUpdateNodePresentationValidation(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	node, err := nodes.UpsertNode(t.Context(), "presentation-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	handler := NewNodeHandler(nodes, mtsStore)
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "valid", body: `{"alias":"edge","group_id":"` + node.GroupID +
			`","tags":[" edge ","prod"],"is_public":false,"sort_order":2,` +
			`"public_remark":"Customer edge","private_remark":"Rack 2"}`, want: http.StatusOK},
		{name: "negative order", body: `{"group_id":"` + node.GroupID + `","sort_order":-1}`, want: http.StatusBadRequest},
		{name: "invalid tag", body: `{"group_id":"` + node.GroupID + `","tags":["bad,tag"]}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/"+node.ID,
				strings.NewReader(test.body))
			request.SetPathValue("id", node.ID)
			response := httptest.NewRecorder()
			handler.HandleUpdateNode(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestHandleSortNodes(t *testing.T) {
	sqliteStore, mtsStore := setupNodeTestDB(t)
	nodes := store.NewNodeStore(sqliteStore.DB)
	first, err := nodes.UpsertNode(t.Context(), "first", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create first node: %v", err)
	}
	second, err := nodes.UpsertNode(t.Context(), "second", "127.0.0.2", 22)
	if err != nil {
		t.Fatalf("create second node: %v", err)
	}
	handler := NewNodeHandler(nodes, mtsStore)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/sort",
		strings.NewReader(`{"ids":["`+second.ID+`","`+first.ID+`"]}`))
	response := httptest.NewRecorder()
	handler.HandleSortNodes(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("sort status = %d, body = %s", response.Code, response.Body.String())
	}

	invalid := httptest.NewRecorder()
	handler.HandleSortNodes(invalid, httptest.NewRequest(http.MethodPut, "/api/v1/nodes/sort",
		strings.NewReader(`{"ids":[]}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort status = %d", invalid.Code)
	}

	invalidJSON := httptest.NewRecorder()
	handler.HandleSortNodes(invalidJSON, httptest.NewRequest(http.MethodPut, "/api/v1/nodes/sort",
		strings.NewReader(`{"ids":`)))
	if invalidJSON.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON sort status = %d", invalidJSON.Code)
	}

	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	failed := httptest.NewRecorder()
	handler.HandleSortNodes(failed, httptest.NewRequest(http.MethodPut, "/api/v1/nodes/sort",
		strings.NewReader(`{"ids":["`+first.ID+`","`+second.ID+`"]}`)))
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed sort status = %d, body = %s", failed.Code, failed.Body.String())
	}
}
