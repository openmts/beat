package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func agentReportRequest(
	t *testing.T,
	nodes *store.NodeStore,
	name string,
	body string,
) *http.Request {
	t.Helper()
	node, err := nodes.UpsertNode(t.Context(), name, "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create report node: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/nodes/report", strings.NewReader(body))
	identity := model.AgentIdentity{
		NodeID: node.ID, NodeName: node.Name, Mode: model.AgentCredentialActive,
	}
	return request.WithContext(middleware.WithAgentIdentity(request.Context(), identity))
}

func setupNodeTestDB(t *testing.T) (*store.SQLiteStore, *store.MTSStore) {
	t.Helper()
	s, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	tmpDir := t.TempDir()
	mtsPath := filepath.Join(tmpDir, "mts")
	mtsStore, err := store.NewMTSStore(mtsPath)
	if err != nil {
		t.Fatalf("failed to create mts store: %v", err)
	}
	t.Cleanup(func() {
		_ = mtsStore.Close()
	})

	return s, mtsStore
}

func disableFK(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatalf("failed to disable foreign keys: %v", err)
	}
}

func enableFK(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}
}

func TestHandleListNodes(t *testing.T) {
	s, mtsStore := setupNodeTestDB(t)
	ctx := context.Background()
	nodeStore := store.NewNodeStore(s.DB)
	h := NewNodeHandler(nodeStore, mtsStore)

	disableFK(t, s.DB)
	_, _ = nodeStore.UpsertNode(ctx, "test-node", "192.168.0.1", 22)
	enableFK(t, s.DB)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHandleListNodesWithoutMetricsStore(t *testing.T) {
	s, _ := setupNodeTestDB(t)
	nodeStore := store.NewNodeStore(s.DB)
	disableFK(t, s.DB)
	if _, err := nodeStore.UpsertNode(context.Background(), "node", "127.0.0.1", 22); err != nil {
		t.Fatalf("create node: %v", err)
	}
	enableFK(t, s.DB)
	request := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	response := httptest.NewRecorder()
	NewNodeHandler(nodeStore, nil).HandleListNodes(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"metrics":{}`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHandleGetNode(t *testing.T) {
	t.Run("gets existing node", func(t *testing.T) {
		s, mtsStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mtsStore)

		disableFK(t, s.DB)
		node, err := nodeStore.UpsertNode(ctx, "get-node", "192.168.0.1", 22)
		enableFK(t, s.DB)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID, nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", node.ID)
		w := httptest.NewRecorder()

		h.HandleGetNode(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("non-existent node returns 404", func(t *testing.T) {
		s, mtsStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mtsStore)

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/nonexistent", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		h.HandleGetNode(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})
}

func TestHandleUpdateNode(t *testing.T) {
	s, mongoStore := setupNodeTestDB(t)
	ctx := context.Background()
	nodeStore := store.NewNodeStore(s.DB)
	h := NewNodeHandler(nodeStore, mongoStore)

	defaultGroup, err := store.NewGroupStore(s.DB).GetDefaultGroup(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	disableFK(t, s.DB)
	node, err := nodeStore.UpsertNode(ctx, "update-node", "192.168.0.1", 22)
	enableFK(t, s.DB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := `{"alias": "new-alias", "group_id": "` + defaultGroup.ID + `", "ssh_public_key": "ssh-ed25519 assigned-key"}`
	req := httptest.NewRequest(http.MethodPut, "/api/nodes/"+node.ID, strings.NewReader(body))
	req = req.WithContext(ctx)
	req.SetPathValue("id", node.ID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleUpdateNode(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ssh-ed25519 assigned-key") {
		t.Errorf("response does not include assigned SSH key: %s", w.Body.String())
	}
}

func TestHandleUpdateMissingNode(t *testing.T) {
	s, mtsStore := setupNodeTestDB(t)
	group, err := store.NewGroupStore(s.DB).GetDefaultGroup(context.Background())
	if err != nil {
		t.Fatalf("get default group: %v", err)
	}
	body := `{"alias":"missing","group_id":"` + group.ID + `"}`
	request := httptest.NewRequest(http.MethodPut, "/api/nodes/missing", strings.NewReader(body))
	request.SetPathValue("id", "missing")
	response := httptest.NewRecorder()
	NewNodeHandler(store.NewNodeStore(s.DB), mtsStore).HandleUpdateNode(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestHandleNodeReportWithoutMetricsStore(t *testing.T) {
	s, _ := setupNodeTestDB(t)
	disableFK(t, s.DB)
	nodes := store.NewNodeStore(s.DB)
	request := agentReportRequest(t, nodes, "node",
		`{"name":"node","host":"127.0.0.1","port":22,"metrics":{"cpu":1}}`)
	response := httptest.NewRecorder()
	NewNodeHandler(nodes, nil).HandleNodeReport(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestHandleDeleteNode(t *testing.T) {
	s, mongoStore := setupNodeTestDB(t)
	ctx := context.Background()
	nodeStore := store.NewNodeStore(s.DB)
	h := NewNodeHandler(nodeStore, mongoStore)

	disableFK(t, s.DB)
	node, err := nodeStore.UpsertNode(ctx, "delete-node", "192.168.0.1", 22)
	enableFK(t, s.DB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/nodes/"+node.ID, nil)
	req = req.WithContext(ctx)
	req.SetPathValue("id", node.ID)
	w := httptest.NewRecorder()

	h.HandleDeleteNode(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestHandleNodeReport(t *testing.T) {
	t.Run("with metrics", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		disableFK(t, s.DB)

		body := `{"name":"report-node","host":"10.0.0.1","port":22,
			"system":{"cpu_model":"Test CPU","os":"linux","platform":"ubuntu","os_version":"24.04","kernel":"6.8.0","arch":"x86_64","virtualization":"kvm guest","agent_version":"1.2.3"},
			"metrics":{"cpu":50,"cpu_used":4,"cpu_total":8,"memory":75,"memory_used":600,"memory_total":800,
			"disk_used":40,"disk_total":100,"disk_read":10,"disk_write":5,"net_recv":100,"net_sent":50,
			"net_recv_total":1000,"net_sent_total":500,"swap":25,"swap_used":25,"swap_total":100,
			"load1":1,"load5":2,"load15":3,"uptime":3600,"processes":42,"tcp_connections":7,"udp_connections":3}}`
		req := agentReportRequest(t, nodeStore, "report-node", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleNodeReport(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
		}
		if err := mongoStore.Flush(ctx); err != nil {
			t.Fatalf("flush metrics: %v", err)
		}
		listRequest := httptest.NewRequest(http.MethodGet, "/api/nodes", nil).WithContext(ctx)
		listResponse := httptest.NewRecorder()
		h.HandleListNodes(listResponse, listRequest)
		if listResponse.Code != http.StatusOK ||
			!strings.Contains(listResponse.Body.String(), `"cpu_model":"Test CPU"`) ||
			!strings.Contains(listResponse.Body.String(), `"agent_version":"1.2.3"`) ||
			!strings.Contains(listResponse.Body.String(), `"cpu_used":4`) ||
			!strings.Contains(listResponse.Body.String(), `"cpu_total":8`) ||
			!strings.Contains(listResponse.Body.String(), `"memory_used":600`) ||
			!strings.Contains(listResponse.Body.String(), `"memory_total":800`) ||
			!strings.Contains(listResponse.Body.String(), `"disk_used":40`) ||
			!strings.Contains(listResponse.Body.String(), `"disk_total":100`) ||
			!strings.Contains(listResponse.Body.String(), `"net_recv_total":1000`) ||
			!strings.Contains(listResponse.Body.String(), `"swap_used":25`) ||
			!strings.Contains(listResponse.Body.String(), `"load15":3`) ||
			!strings.Contains(listResponse.Body.String(), `"uptime":3600`) ||
			!strings.Contains(listResponse.Body.String(), `"processes":42`) ||
			!strings.Contains(listResponse.Body.String(), `"tcp_connections":7`) {
			t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
		}
	})

	t.Run("without metrics", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		disableFK(t, s.DB)

		body := `{"name": "no-metrics-node", "host": "10.0.0.2", "port": 22}`
		req := agentReportRequest(t, nodeStore, "no-metrics-node", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleNodeReport(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		body := `not-json`
		req := agentReportRequest(t, nodeStore, "invalid-json-node", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleNodeReport(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		body := `{"host": "10.0.0.3", "port": 22}`
		req := agentReportRequest(t, nodeStore, "bound-node", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleNodeReport(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", w.Code)
		}
	})

	t.Run("missing host returns 400", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		body := `{"name": "no-host-node", "port": 22}`
		req := agentReportRequest(t, nodeStore, "no-host-node", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.HandleNodeReport(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid port returns 400", func(t *testing.T) {
		s, mtsStore := setupNodeTestDB(t)
		nodes := store.NewNodeStore(s.DB)
		request := agentReportRequest(t, nodes, "bad-port",
			`{"name":"bad-port","host":"127.0.0.1","port":70000}`)
		response := httptest.NewRecorder()
		NewNodeHandler(nodes, mtsStore).HandleNodeReport(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})

	t.Run("invalid metrics return 400", func(t *testing.T) {
		s, mtsStore := setupNodeTestDB(t)
		nodes := store.NewNodeStore(s.DB)
		request := agentReportRequest(t, nodes, "bad-metrics",
			`{"name":"bad-metrics","host":"127.0.0.1","port":22,"metrics":{"cpu":101}}`)
		response := httptest.NewRecorder()
		NewNodeHandler(nodes, mtsStore).HandleNodeReport(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})
}

func TestHandleGetNodeMetrics(t *testing.T) {
	t.Run("valid query params", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		disableFK(t, s.DB)
		node, err := nodeStore.UpsertNode(ctx, "metrics-node", "10.0.0.4", 22)
		enableFK(t, s.DB)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID+"/metrics?metric=cpu&from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", node.ID)
		w := httptest.NewRecorder()

		h.HandleGetNodeMetrics(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("uses default metrics when metric is omitted", func(t *testing.T) {
		s, mtsStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mtsStore)

		node, err := nodeStore.UpsertNode(ctx, "default-metrics-node", "10.0.0.8", 22)
		if err != nil {
			t.Fatalf("upsert node: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID+"/metrics?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", node.ID)
		w := httptest.NewRecorder()

		h.HandleGetNodeMetrics(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		for _, metric := range defaultMetrics {
			if !strings.Contains(w.Body.String(), `"`+metric+`"`) {
				t.Errorf("response does not include default metric %q", metric)
			}
		}
	})

	t.Run("invalid from timestamp returns 400", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		disableFK(t, s.DB)
		node, err := nodeStore.UpsertNode(ctx, "metrics-node-2", "10.0.0.5", 22)
		enableFK(t, s.DB)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID+"/metrics?metric=cpu&from=invalid&to=2024-01-02T00:00:00Z", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", node.ID)
		w := httptest.NewRecorder()

		h.HandleGetNodeMetrics(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid to timestamp returns 400", func(t *testing.T) {
		s, mongoStore := setupNodeTestDB(t)
		ctx := context.Background()
		nodeStore := store.NewNodeStore(s.DB)
		h := NewNodeHandler(nodeStore, mongoStore)

		disableFK(t, s.DB)
		node, err := nodeStore.UpsertNode(ctx, "metrics-node-3", "10.0.0.6", 22)
		enableFK(t, s.DB)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID+"/metrics?metric=cpu&from=2024-01-01T00:00:00Z&to=invalid", nil)
		req = req.WithContext(ctx)
		req.SetPathValue("id", node.ID)
		w := httptest.NewRecorder()

		h.HandleGetNodeMetrics(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("missing node returns 404", func(t *testing.T) {
		s, mtsStore := setupNodeTestDB(t)
		h := NewNodeHandler(store.NewNodeStore(s.DB), mtsStore)

		req := httptest.NewRequest(http.MethodGet, "/api/nodes/missing/metrics?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", nil)
		req.SetPathValue("id", "missing")
		w := httptest.NewRecorder()

		h.HandleGetNodeMetrics(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})
}

func TestHandleGetNodeRouting(t *testing.T) {
	s, mongoStore := setupNodeTestDB(t)
	ctx := context.Background()
	nodeStore := store.NewNodeStore(s.DB)
	h := NewNodeHandler(nodeStore, mongoStore)

	disableFK(t, s.DB)
	node, err := nodeStore.UpsertNode(ctx, "routing-node", "10.0.0.7", 22)
	enableFK(t, s.DB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/nodes/{id}", h.HandleGetNode)
	mux.HandleFunc("PUT /api/nodes/{id}", h.HandleUpdateNode)

	req := httptest.NewRequest(http.MethodGet, "/api/nodes/"+node.ID, nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// Ensure all handler methods satisfy http.HandlerFunc.
var _ http.HandlerFunc = (&NodeHandler{}).HandleListNodes

var _ http.HandlerFunc = (&NodeHandler{}).HandleGetNode

var _ http.HandlerFunc = (&NodeHandler{}).HandleUpdateNode

var _ http.HandlerFunc = (&NodeHandler{}).HandleDeleteNode

var _ http.HandlerFunc = (&NodeHandler{}).HandleNodeReport

var _ http.HandlerFunc = (&NodeHandler{}).HandleGetNodeMetrics
