package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/service"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
	"github.com/beat/backend/internal/trafficreport"
)

func TestNotificationBrowserFixture(t *testing.T) {
	static, ready := os.Getenv("BEAT_NOTIFICATION_STATIC"), os.Getenv("BEAT_NOTIFICATION_READY")
	if static == "" || ready == "" {
		t.Skip("isolated notification browser fixture not requested")
	}
	var received atomic.Int64
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		received.Add(1)
		if request.URL.Path == "/reject" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	router := notificationBrowserRouter(t)
	stop := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.Handle("/api/", router.ServeHandler())
	mux.HandleFunc("POST /fixture/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		once.Do(func() { close(stop) })
	})
	mux.HandleFunc("GET /fixture/received", func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]int64{"count": received.Load()}); err != nil {
			t.Errorf("write receiver count: %v", err)
		}
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
	data, err := json.Marshal(map[string]string{"url": server.URL, "receiver": receiver.URL, "bootstrap": "notification-fixture-bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, data, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stop:
	case <-time.After(15 * time.Minute):
		t.Fatal("browser fixture exceeded its lifetime")
	}
}

func notificationBrowserRouter(t *testing.T) *Router {
	t.Helper()
	directory := t.TempDir()
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mts, err := store.NewMTSStore(filepath.Join(directory, "mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mts.Close() })
	secrets := testutil.OpenSecrets(t, database.DB, directory)
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: secrets, LegacyToken: "notification-fixture-bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: database.DB, Secrets: secrets, Sender: notification.NewDispatcher()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- queue.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		queue.Close()
		if err := <-done; err != nil {
			t.Errorf("notification worker: %v", err)
		}
	})
	nodes, events := store.NewNodeStore(database.DB), store.NewAlertEventStore(database.DB)
	reports := trafficreport.NewService(trafficreport.Config{Schedules: store.NewTrafficReportScheduleStore(database.DB), Nodes: nodes, Metrics: mts, Queue: queue})
	return NewRouter(nodes, store.NewGroupStore(database.DB), store.NewSSHKeyStore(database.DB), store.NewAlertRuleStore(database.DB), store.NewAlertChannelStore(database.DB), events, mts,
		WithAdminSecurity(security), WithAdminToken("notification-fixture-bootstrap"), WithNotificationQueue(queue), WithAlertNotifications(service.NewAlertNotificationService(events, queue)),
		WithTrafficReportService(reports), WithSiteSettingsStore(store.NewSiteSettingsStore(database.DB)), WithCustomMetricStore(store.NewCustomMetricStore(database.DB)))
}
