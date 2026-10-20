package agentrollouthttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

type requestInput struct {
	method, path, node string
	body               any
	headers            map[string]string
	anonymous          bool
}

func (f *fixture) call(t *testing.T, input requestInput) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if raw, ok := input.body.([]byte); ok {
		body = raw
	} else if input.body != nil {
		var err error
		body, err = json.Marshal(input.body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(input.method, input.path, bytes.NewReader(body))
	if input.node != "" {
		r = r.WithContext(middleware.WithAgentIdentity(r.Context(), f.identities[input.node]))
	} else if !input.anonymous {
		r = r.WithContext(middleware.WithAdminPrincipal(r.Context(), model.AdminPrincipal{User: model.AdminUser{ID: f.actor.UserID}, Session: model.AdminSession{ID: f.actor.SessionID}}))
	}
	for key, value := range input.headers {
		r.Header.Set(key, value)
	}
	mux := http.NewServeMux()
	New(f.s).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers", w.Header())
	}
	return w
}

func result[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data T   `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 200 {
		t.Fatal(envelope.Code)
	}
	return envelope.Data
}

func TestOwnerHTTPReleaseRolloutAndAgentFlow(t *testing.T) {
	f := newFixture(t)
	ids := f.nodes(t, 1)
	release := result[agentrollout.Release](t, f.call(t, requestInput{method: "POST", path: "/api/v1/admin/agent-releases/import", body: f.signed}))
	result[any](t, f.call(t, requestInput{method: "PUT", path: "/api/v1/admin/agent-releases/" + release.ID + "/artifacts/linux-amd64", body: f.artifact, headers: map[string]string{"Content-Type": "application/octet-stream"}}))
	detail := result[agentrollout.ReleaseDetail](t, f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-releases/" + release.ID}))
	if len(detail.Artifacts) != 1 || detail.Artifacts[0].State != "ready" {
		t.Fatal(detail)
	}
	result[agentrollout.Page[agentrollout.Release]](t, f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-releases?limit=1"}))
	rollout := result[agentrollout.Rollout](t, f.call(t, requestInput{method: "POST", path: "/api/v1/admin/agent-rollouts", body: agentrollout.CreateInput{Name: "HTTP", ReleaseID: release.ID, NodeIDs: ids}}))
	root := "/api/v1/admin/agent-rollouts/" + rollout.ID
	result[agentrollout.Rollout](t, f.call(t, requestInput{method: "GET", path: root}))
	result[agentrollout.Page[agentrollout.Rollout]](t, f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-rollouts"}))
	result[agentrollout.Page[agentrollout.Target]](t, f.call(t, requestInput{method: "GET", path: root + "/targets"}))
	result[any](t, f.call(t, requestInput{method: "POST", path: root + "/promote"}))
	lease := result[*protocol.Assignment](t, f.call(t, requestInput{method: "POST", path: "/api/v1/agents/rollout/lease", node: ids[0], body: capabilities()}))
	if lease == nil || lease.Action != "upgrade" || lease.ServerSchema != 14 {
		t.Fatal(lease)
	}
	next := result[*protocol.Assignment](t, f.call(t, requestInput{method: "POST", path: "/api/v1/agents/rollout/lease", node: ids[0], body: capabilities()}))
	if next != nil {
		t.Fatal("lost response regenerated nonce")
	}
	path := "/api/v1/agents/rollout/artifacts/" + lease.ID + "/linux-amd64"
	w := f.call(t, requestInput{method: "GET", path: path, node: ids[0], headers: map[string]string{"X-Beat-Rollout-Nonce": lease.Nonce, "Range": "bytes=0-3"}})
	if w.Code != 206 || w.Body.String() != string(f.artifact[:4]) || w.Header().Get("Content-Length") != "4" || w.Header().Get("Content-Range") != fmt.Sprintf("bytes 0-3/%d", len(f.artifact)) {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	tag := w.Header().Get("ETag")
	w = f.call(t, requestInput{method: "GET", path: path, node: ids[0], headers: map[string]string{"X-Beat-Rollout-Nonce": lease.Nonce, "If-None-Match": tag}})
	if w.Code != 304 {
		t.Fatal(w.Code)
	}
	result[any](t, f.call(t, requestInput{method: "POST", path: "/api/v1/agents/rollout/observations", node: ids[0], body: f.report(t, lease)}))
	for _, action := range []string{"pause", "resume", "abort"} {
		result[any](t, f.call(t, requestInput{method: "POST", path: root + "/" + action, body: map[string]bool{"acknowledge_uncertainty": true}}))
	}
	result[any](t, f.call(t, requestInput{method: "POST", path: "/api/v1/admin/agent-releases/" + release.ID + "/revoke"}))
}

func TestHTTPRejectionsAndMetadataDoesNotRequireRecent(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	result[agentrollout.ReleaseDetail](t, f.call(t, requestInput{method: "GET", path: "/api/v1/admin/agent-releases/" + release.ID}))
	cases := []struct {
		input  requestInput
		status int
	}{
		{requestInput{method: "POST", path: "/api/v1/admin/agent-releases/" + release.ID + "/revoke"}, 428},
		{requestInput{method: "GET", path: "/api/v1/admin/agent-releases", anonymous: true}, 403},
		{requestInput{method: "GET", path: "/api/v1/admin/agent-releases?limit=bad"}, 400},
		{requestInput{method: "GET", path: "/api/v1/admin/agent-rollouts?cursor=%%%"}, 400},
		{requestInput{method: "GET", path: "/api/v1/admin/agent-rollouts/x/targets?limit=-1"}, 400},
		{requestInput{method: "POST", path: "/api/v1/admin/agent-releases/import", body: []byte("{bad")}, 400},
		{requestInput{method: "POST", path: "/api/v1/admin/agent-rollouts", body: []byte(`{"unknown":1}`)}, 400},
		{requestInput{method: "POST", path: "/api/v1/admin/agent-rollouts/x/resume", body: []byte(`{} {}`)}, 400},
		{requestInput{method: "PUT", path: "/api/v1/admin/agent-releases/x/artifacts/x"}, 400},
		{requestInput{method: "POST", path: "/api/v1/agents/rollout/lease"}, 403},
		{requestInput{method: "POST", path: "/api/v1/agents/rollout/observations"}, 403},
		{requestInput{method: "GET", path: "/api/v1/agents/rollout/artifacts/x/x"}, 403},
	}
	for _, tc := range cases {
		w := f.call(t, tc.input)
		if w.Code != tc.status {
			t.Fatal(tc.input.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "fixture-bundle") {
			t.Fatal("secret leak")
		}
	}
}
