package agentrollouthttp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrolloutremote"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func TestRemoteReleaseSyncOverridesShortServerWriteTimeout(t *testing.T) {
	f := newFixture(t)
	assets := []struct {
		id   int64
		name string
		body []byte
		path string
	}{
		{id: 1, name: agentrolloutremote.ManifestAssetName, body: f.signed.Payload, path: "/releases/assets/1"},
		{id: 2, name: agentrolloutremote.SignatureAssetName, body: f.signed.Signature, path: "/releases/assets/2"},
		{id: 3, name: agentrolloutremote.SigstoreAssetName, body: f.signed.SigstoreBundle, path: "/releases/assets/3"},
		{id: 4, name: "beat-agent", body: f.artifact, path: "/releases/assets/4"},
	}
	releaseAssets := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		releaseAssets = append(releaseAssets, map[string]any{"id": asset.id, "name": asset.name, "size": len(asset.body)})
	}
	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/tags/v2.0.0"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 10, "tag_name": "v2.0.0", "name": "Beat", "draft": false, "prerelease": false,
				"published_at": "2026-10-07T11:00:00Z", "assets": releaseAssets,
			})
		case strings.Contains(r.URL.Path, "/releases/assets/"):
			time.Sleep(50 * time.Millisecond)
			for _, asset := range assets {
				if strings.HasSuffix(r.URL.Path, "/"+strconv.FormatInt(asset.id, 10)) {
					_, _ = w.Write(asset.body)
					return
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remoteServer.Close)
	remote, err := agentrolloutremote.New(agentrolloutremote.Config{Rollout: f.s, APIURL: remoteServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewWithRemote(f.s, remote).Register(mux)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := model.AdminPrincipal{User: model.AdminUser{ID: f.actor.UserID}, Session: model.AdminSession{ID: f.actor.SessionID}}
		mux.ServeHTTP(w, r.WithContext(middleware.WithAdminPrincipal(r.Context(), principal)))
	}))
	server.Config.WriteTimeout = 10 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	response, err := server.Client().Post(server.URL+"/api/v1/admin/agent-releases/remote/v2.0.0/sync", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s err=%v", response.StatusCode, body, err)
	}
}

func TestRemoteReleaseSyncDeadlineFailureDoesNotCallRemote(t *testing.T) {
	f := newFixture(t)
	remote, err := agentrolloutremote.New(agentrolloutremote.Config{Rollout: f.s, APIURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewWithRemote(f.s, remote).Register(mux)
	request := f.deadlineRequest("POST", "/api/v1/admin/agent-releases/remote/v2.0.0/sync", nil)
	response := deadlineFaultWriter{ResponseRecorder: httptest.NewRecorder(), writeErr: errors.New("fixture deadline failure")}
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
