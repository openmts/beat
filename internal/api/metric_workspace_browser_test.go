package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

func TestMetricWorkspaceBrowserFixture(t *testing.T) {
	static, ready := os.Getenv("BEAT_WORKSPACE_STATIC"), os.Getenv("BEAT_WORKSPACE_READY")
	if static == "" || ready == "" {
		t.Skip("isolated browser fixture not requested")
	}
	router := metricWorkspaceBrowserRouter(t)
	stop := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.Handle("/api/", router.ServeHandler())
	mux.HandleFunc("POST /fixture/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		once.Do(func() { close(stop) })
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/assets/") {
			http.FileServer(http.Dir(static)).ServeHTTP(w, request)
			return
		}
		http.ServeFile(w, request, filepath.Join(static, "index.html"))
	})
	server := httptest.NewServer(middleware.SecurityHeaders(mux))
	t.Cleanup(server.Close)
	data, err := json.Marshal(map[string]string{"url": server.URL, "bootstrap": "workspace-fixture-bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, data, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stop:
	case <-time.After(20 * time.Minute):
		t.Fatal("browser fixture exceeded its lifetime")
	}
}

func metricWorkspaceBrowserRouter(t *testing.T) *Router {
	t.Helper()
	directory := t.TempDir()
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mts, err := store.NewMTSStore(filepath.Join(directory, "beat_mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mts.Close() })
	secrets := testutil.OpenSecrets(t, database.DB, directory)
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: secrets, LegacyToken: "workspace-fixture-bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	nodes, catalog := store.NewNodeStore(database.DB), store.NewMetricCatalogStore(database.DB)
	seedWorkspaceBrowserNodes(t, nodes, mts)
	return NewRouter(nodes, store.NewGroupStore(database.DB), store.NewSSHKeyStore(database.DB),
		store.NewAlertRuleStore(database.DB), store.NewAlertChannelStore(database.DB), store.NewAlertEventStore(database.DB), mts,
		WithAdminSecurity(security), WithAdminToken("workspace-fixture-bootstrap"), WithMetricCatalogStore(catalog),
		WithSiteSettingsStore(store.NewSiteSettingsStore(database.DB)),
		WithCustomMetricStore(store.NewCustomMetricStore(database.DB)),
		WithMetricDashboards(store.NewDashboardStore(database.DB), handler.NewDashboardPolicyReader(catalog, nodes)))
}

func seedWorkspaceBrowserNodes(t *testing.T, nodes *store.NodeStore, mts *store.MTSStore) {
	t.Helper()
	for index, name := range []string{"edge-ap-01", "database-eu-01", "gateway-us-01"} {
		node, err := nodes.UpsertNode(t.Context(), name, "192.0.2.10", 22)
		if err != nil {
			t.Fatal(err)
		}
		for sample := range 120 {
			captured := model.NowUTC().Add(time.Duration(sample-119) * 30 * time.Second)
			cpu := 28 + float64(index)*8 + math.Sin(float64(sample)/6)*12
			metrics := model.NodeMetrics{CPU: cpu, CPUTotal: 4, CPUUsed: cpu / 25, Memory: 42 + float64(index)*7,
				MemoryUsed: 3e9, MemoryTotal: 8e9, Disk: 36, DiskUsed: 36e9, DiskTotal: 100e9,
				NetRecv: 6e6 + math.Sin(float64(sample)/4)*3e6, NetSent: 2e6, NetRecvTotal: float64(sample) * 18e7, NetSentTotal: float64(sample) * 6e7,
				Uptime: 1000 + float64(sample)*30, Processes: 140}
			if err := mts.WriteNodeMetrics(t.Context(), store.NodeMetricSample{NodeID: node.ID, Metrics: metrics, Timestamp: captured}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mts.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
}
