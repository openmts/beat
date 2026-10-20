package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func metricErasureRouter(t *testing.T) (*Router, *store.SQLiteStore) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	router := setupSessionRouterWithOptions(t,
		WithMetricErasureStore(store.NewMetricErasureStore(sqlite.DB), nil))
	return router, sqlite
}

func TestMetricErasureRoutesRequireOwnerRecentAuth(t *testing.T) {
	router, _ := metricErasureRouter(t)
	const jobID = "aaaaaaaa-1111-1111-1111-111111111111"
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/admin/metric-erasure/jobs", ""},
		{http.MethodGet, "/api/v1/admin/metric-erasure/jobs/" + jobID, ""},
		{http.MethodPost, "/api/v1/admin/metric-erasure/jobs", `{}`},
		{http.MethodPost, "/api/v1/admin/metric-erasure/jobs/" + jobID + "/retry", ""},
		{http.MethodPost, "/api/v1/admin/metric-erasure/jobs/" + jobID + "/cancel", ""},
	}
	for _, route := range routes {
		anonymous := requestRouter(router, route.method, route.path, route.body, "", "http://example.com")
		if anonymous.Code != http.StatusUnauthorized && anonymous.Code != http.StatusForbidden {
			t.Fatalf("%s %s anonymous status = %d", route.method, route.path, anonymous.Code)
		}
	}
	cookie := bootstrapTerminalOwner(t, router)
	for _, route := range routes {
		fresh := requestRouterWithCookie(router, route.method, route.path, route.body, cookie, "http://example.com")
		if fresh.Code != http.StatusPreconditionRequired {
			t.Fatalf("%s %s status = %d body = %s", route.method, route.path, fresh.Code, fresh.Body.String())
		}
	}
}

func TestNodeDeletionBecomesCancelableErasureJob(t *testing.T) {
	router, sqlite := metricErasureRouter(t)
	cookie := bootstrapTerminalOwner(t, router)
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauth status = %d", reauth.Code)
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes",
		`{"name":"erasure-node","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`,
		cookie, "http://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	nodeID := createdNodeID(t, created.Body.Bytes())
	path := "/api/v1/nodes/" + nodeID
	rejected := requestRouterWithCookie(router, http.MethodDelete, path,
		`{"request_id":"aaaaaaaa-1111-1111-1111-111111111111","confirmation":"wrong-name"}`,
		cookie, "http://example.com")
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status = %d body = %s", rejected.Code, rejected.Body.String())
	}
	deleted := requestRouterWithCookie(router, http.MethodDelete, path,
		`{"request_id":"aaaaaaaa-1111-1111-1111-111111111111","confirmation":"erasure-node"}`,
		cookie, "http://example.com")
	if deleted.Code != http.StatusAccepted || !strings.Contains(deleted.Body.String(), `"state":"queued"`) {
		t.Fatalf("delete status = %d body = %s", deleted.Code, deleted.Body.String())
	}
	hidden := requestRouterWithCookie(router, http.MethodGet, path, "", cookie, "http://example.com")
	if hidden.Code != http.StatusNotFound {
		t.Fatalf("tombstoned node status = %d", hidden.Code)
	}
	var jobID, state string
	if err := sqlite.DB.QueryRow("SELECT id, state FROM metric_erasure_jobs ORDER BY rowid DESC LIMIT 1").
		Scan(&jobID, &state); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if state != "queued" {
		t.Fatalf("job state = %s", state)
	}
	canceled := requestRouterWithCookie(router, http.MethodPost,
		"/api/v1/admin/metric-erasure/jobs/"+jobID+"/cancel", "", cookie, "http://example.com")
	if canceled.Code != http.StatusOK || !strings.Contains(canceled.Body.String(), `"state":"canceled"`) {
		t.Fatalf("cancel status = %d body = %s", canceled.Code, canceled.Body.String())
	}
	restored := requestRouterWithCookie(router, http.MethodGet, path, "", cookie, "http://example.com")
	if restored.Code != http.StatusOK {
		t.Fatalf("restored node status = %d body = %s", restored.Code, restored.Body.String())
	}
}
