package api

import (
	"fmt"
	"testing"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestAuthenticatedRolloutRoutesPerformDomainWritesOnce(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	nodeID, token := f.node(t)
	status := rolloutResponse[agentrollout.Status](t, f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"}))
	if !status.Ready {
		t.Fatal(status)
	}
	release := rolloutResponse[agentrollout.Release](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: rolloutTestOrigin, body: f.signed}))
	releasePath := "/api/v1/admin/agent-releases/" + release.ID
	rolloutResponse[any](t, f.call(t, rolloutCall{method: "PUT", path: releasePath + "/artifacts/linux-amd64", origin: rolloutTestOrigin, body: f.artifact, headers: map[string]string{"Content-Type": "application/octet-stream"}}))
	detail := rolloutResponse[agentrollout.ReleaseDetail](t, f.call(t, rolloutCall{method: "GET", path: releasePath}))
	if len(detail.Artifacts) != 1 || detail.Artifacts[0].State != "ready" {
		t.Fatal(detail)
	}
	rollout := rolloutResponse[agentrollout.Rollout](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-rollouts", origin: rolloutTestOrigin, body: agentrollout.CreateInput{Name: "central auth", ReleaseID: release.ID, NodeIDs: []string{nodeID}}}))
	path := "/api/v1/admin/agent-rollouts/" + rollout.ID
	rolloutResponse[any](t, f.call(t, rolloutCall{method: "POST", path: path + "/promote", origin: rolloutTestOrigin}))
	lease := rolloutResponse[*protocol.Assignment](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/agents/rollout/lease", bearer: token, anonymous: true, body: rolloutCaps()}))
	if lease == nil || lease.NodeID != nodeID {
		t.Fatal(lease)
	}
	download := f.call(t, rolloutCall{method: "GET", path: "/api/v1/agents/rollout/artifacts/" + lease.ID + "/linux-amd64", bearer: token, anonymous: true, headers: map[string]string{"X-Beat-Rollout-Nonce": lease.Nonce, "Range": "bytes=0-3"}})
	if download.Code != 206 || download.Body.String() != string(f.artifact[:4]) || download.Header().Get("Content-Range") != fmt.Sprintf("bytes 0-3/%d", len(f.artifact)) {
		t.Fatal(download.Code, download.Body.String(), download.Header())
	}
	observation := protocol.Observation{AssignmentID: lease.ID, Nonce: lease.Nonce, Version: "2.0.0", ReportAt: f.now, Outcome: "healthy"}
	rolloutResponse[any](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/agents/rollout/observations", bearer: token, anonymous: true, body: observation}))
	targets := rolloutResponse[agentrollout.Page[agentrollout.Target]](t, f.call(t, rolloutCall{method: "GET", path: path + "/targets"}))
	if len(targets.Items) != 1 || targets.Items[0].LastReportAt == nil {
		t.Fatal(targets)
	}
	for _, action := range []string{"pause", "resume", "abort"} {
		rolloutResponse[any](t, f.call(t, rolloutCall{method: "POST", path: path + "/" + action, origin: rolloutTestOrigin, body: map[string]bool{"acknowledge_uncertainty": true}}))
	}
	rolloutResponse[any](t, f.call(t, rolloutCall{method: "POST", path: releasePath + "/revoke", origin: rolloutTestOrigin}))
	for _, action := range []string{"release.import", "release.artifact.import", "rollout.create", "rollout.promote", "rollout.paused", "rollout.resume", "rollout.aborted", "release.revoke"} {
		var count int
		if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action=? AND outcome='success'", action).Scan(&count); err != nil || count != 1 {
			t.Fatal(action, count, err)
		}
	}
	var generic int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.mutation'").Scan(&generic); err != nil || generic != 0 {
		t.Fatal("duplicate generic success audit", generic, err)
	}
}

func TestRolloutOwnerRoutesRejectStaleAuthorityAndCrossSiteWrites(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	for _, origin := range []string{"", "https://other.example.test"} {
		response := f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: origin, body: f.signed})
		if response.Code != 403 {
			t.Fatal(origin, response.Code)
		}
	}
	for _, input := range []rolloutCall{{method: "GET", path: "/api/v1/admin/agent-rollout-status", anonymous: true, bearer: "bootstrap-admin"}, {method: "GET", path: "/api/v1/admin/agent-rollout-status", cookie: "invalid-session"}} {
		if response := f.call(t, input); response.Code != 401 {
			t.Fatal(response.Code)
		}
	}
	if _, err := f.db.DB.Exec("UPDATE admin_users SET role='admin' WHERE id=?", f.owner.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"}); response.Code != 403 {
		t.Fatal(response.Code)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_users SET role='owner' WHERE id=?", f.owner.Principal.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL WHERE id=?", f.owner.Principal.Session.ID); err != nil {
		t.Fatal(err)
	}
	rolloutResponse[agentrollout.Status](t, f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"}))
	response := f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: rolloutTestOrigin, body: f.signed})
	if response.Code != 428 {
		t.Fatal(response.Code)
	}
	var failed int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='release.import' AND resource_type='release' AND outcome='failure'").Scan(&failed); err != nil || failed != 1 {
		t.Fatal(failed, err)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET revoked_at=? WHERE id=?", f.now, f.owner.Principal.Session.ID); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"}); response.Code != 401 {
		t.Fatal(response.Code)
	}
}

func TestAgentRolloutRoutesRequireActiveNodeCredentialsIndependentlyOfRouterFlag(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	_, token := f.node(t)
	f.router.agentAuthEnabled = false
	routes := []string{"/api/v1/agents/rollout/lease", "/api/v1/agents/rollout/observations", "/api/v1/agents/rollout/artifacts/attempt/artifact"}
	for _, path := range routes {
		method := "POST"
		if path == routes[2] {
			method = "GET"
		}
		for _, credential := range []string{"", "legacy-agent", "bootstrap-admin", "stale-token"} {
			response := f.call(t, rolloutCall{method: method, path: path, bearer: credential, body: rolloutCaps()})
			if response.Code != 401 {
				t.Fatal(path, credential, response.Code)
			}
		}
	}
	rolloutResponse[*protocol.Assignment](t, f.call(t, rolloutCall{method: "POST", path: routes[0], bearer: token, anonymous: true, body: rolloutCaps()}))
	if _, err := f.db.DB.Exec("UPDATE nodes SET agent_token_revoked_at=?", f.now); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, rolloutCall{method: "POST", path: routes[0], bearer: token, anonymous: true, body: rolloutCaps()}); response.Code != 401 {
		t.Fatal(response.Code)
	}
}

func TestDisabledRolloutStatusNeedsNoV15TablesOrRecentProof(t *testing.T) {
	f := newRolloutRouteFixture(t, false)
	if _, err := f.db.DB.Exec("DROP TABLE agent_release_signature_state"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	value := rolloutResponse[agentrollout.Status](t, f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"}))
	if value.Enabled || value.Ready || value.Reason != "disabled" {
		t.Fatal(value)
	}
	if response := f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-releases"}); response.Code != 404 {
		t.Fatal(response.Code)
	}
}
