package agentrollouthttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beat/backend/internal/agentrolloutremote"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func TestRemoteReleaseRoutesFailClosedAndValidatePaging(t *testing.T) {
	f := newFixture(t)
	if response := f.remoteCall(t, nil, "/api/v1/admin/agent-releases/remote"); response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured remote status = %d, body = %s", response.Code, response.Body.String())
	}
	remote, err := agentrolloutremote.New(agentrolloutremote.Config{Rollout: f.s, APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"page=0", "page=101", "page=bad", "page=1&page=2", "unknown=1"} {
		response := f.remoteCall(t, remote, "/api/v1/admin/agent-releases/remote?"+query)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, body = %s", query, response.Code, response.Body.String())
		}
	}
}

func TestRemoteReleaseRateLimitPreservesRetryAfter(t *testing.T) {
	f := newFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	remote, err := agentrolloutremote.New(agentrolloutremote.Config{Rollout: f.s, APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	response := f.remoteCall(t, remote, "/api/v1/admin/agent-releases/remote")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "7" {
		t.Fatalf("status = %d, Retry-After = %q, body = %s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func (f *fixture) remoteCall(t *testing.T, remote *agentrolloutremote.Service, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewWithRemote(f.s, remote).Register(mux)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{
		User: model.AdminUser{ID: f.actor.UserID}, Session: model.AdminSession{ID: f.actor.SessionID},
	}))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}
