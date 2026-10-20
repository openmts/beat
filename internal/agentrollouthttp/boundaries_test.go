package agentrollouthttp

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func TestDownloadHTTPRejectsInvalidRangesNoncesAndRevocation(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	identity := f.identities[targets[0].NodeID]
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), identity, capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	path := "/api/v1/agents/rollout/artifacts/" + assignment.ID + "/" + assignment.Artifact.ID
	for _, value := range []string{"units=0-3", "bytes=0-1,2-3", "bytes=--", "bytes=a-b", "bytes=-", "bytes=0-" + strings.Repeat("1", 130)} {
		w := f.call(t, requestInput{method: "GET", path: path, node: identity.NodeID, headers: map[string]string{"X-Beat-Rollout-Nonce": assignment.Nonce, "Range": value}})
		if w.Code != 400 {
			t.Fatal(value, w.Code)
		}
	}
	w := f.call(t, requestInput{method: "GET", path: path + "?nonce=" + assignment.Nonce, node: identity.NodeID, headers: map[string]string{"X-Beat-Rollout-Nonce": assignment.Nonce}})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = f.call(t, requestInput{method: "GET", path: path, node: identity.NodeID})
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = f.call(t, requestInput{method: "GET", path: path, node: identity.NodeID, headers: map[string]string{"X-Beat-Rollout-Nonce": strings.Repeat("0", len(assignment.Nonce))}})
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = f.call(t, requestInput{method: "GET", path: path, node: identity.NodeID, headers: map[string]string{"X-Beat-Rollout-Nonce": assignment.Nonce, "Range": "bytes=9999-"}})
	if w.Code != 416 {
		t.Fatal(w.Code)
	}
	if err := f.s.RevokeRelease(t.Context(), f.actor, rollout.ReleaseID); err != nil {
		t.Fatal(err)
	}
	w = f.call(t, requestInput{method: "GET", path: path, node: identity.NodeID, headers: map[string]string{"X-Beat-Rollout-Nonce": assignment.Nonce}})
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	for _, route := range []string{"lease", "observations"} {
		w := f.call(t, requestInput{method: "POST", path: "/api/v1/agents/rollout/" + route, node: identity.NodeID, body: []byte("invalid")})
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}

func TestBoundedDecodeAndErrorMapping(t *testing.T) {
	for _, input := range []struct{ body, encoding string }{{"{}", "gzip"}, {strings.Repeat(" ", maximumJSONBytes) + "{}", ""}, {"{} {}", ""}} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(input.body))
		r.Header.Set("Content-Encoding", input.encoding)
		if err := decode(r, &map[string]any{}); err != agentrollout.ErrInvalid {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct {
		err    error
		status int
	}{{agentrollout.ErrBusy, 503}, {agentrollout.ErrDisabled, 404}, {agentrollout.ErrNotFound, 404}, {agentrollout.ErrConflict, 409}, {errors.New("secret-token"), 503}} {
		w := httptest.NewRecorder()
		respond(w, nil, entry.err)
		if w.Code != entry.status || strings.Contains(w.Body.String(), "secret-token") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if !singleRange("bytes=-3") || !singleRange("bytes=3-") {
		t.Fatal("valid range rejected")
	}
}

func TestUploadConcurrencyIsBounded(t *testing.T) {
	f := newFixture(t)
	handler := New(f.s)
	for range cap(handler.uploads) {
		handler.uploads <- struct{}{}
	}
	request := httptest.NewRequest("PUT", "/", bytes.NewReader(f.artifact))
	request.Header.Set("Content-Type", "application/octet-stream")
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{User: model.AdminUser{ID: f.actor.UserID}, Session: model.AdminSession{ID: f.actor.SessionID}}))
	response := httptest.NewRecorder()
	handler.upload(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatal(response.Code, response.Header())
	}
}
