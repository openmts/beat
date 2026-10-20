package agentrollouthttp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
)

func TestDeleteRoutesRejectThenRemoveTerminalResources(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	nodes := f.nodes(t, 1)
	rollout, err := f.s.Create(t.Context(), f.actor, agentrollout.CreateInput{Name: "Delete drill", ReleaseID: release.ID, NodeIDs: nodes, Policy: agentrollout.Policy{CanaryCount: 1, WaveSize: 1, ObservationSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-rollouts/" + rollout.ID}); response.Code != http.StatusConflict {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-releases/" + release.ID}); response.Code != http.StatusConflict {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := f.s.Abort(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-rollouts/" + rollout.ID}); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-releases/" + release.ID}); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-releases/" + release.ID}); response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-releases/" + strings.Repeat("a", 129)}); response.Code != http.StatusBadRequest {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestDeleteRoutesRequireOwnerAuthority(t *testing.T) {
	f := newFixture(t)
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-rollouts/one", anonymous: true}); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := f.call(t, requestInput{method: http.MethodDelete, path: "/api/v1/admin/agent-releases/one", anonymous: true}); response.Code != http.StatusForbidden {
		t.Fatal(response.Code, response.Body.String())
	}
}
