package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/api/middleware"
)

func TestRolloutSessionExpiryAndCredentialRotationAreRechecked(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	nodeID, oldToken := f.node(t)
	fresh, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE nodes SET agent_token_hash=?,agent_token_prefix=? WHERE id=?", fresh.Hash[:], fresh.Prefix, nodeID); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, rolloutCall{method: "POST", path: "/api/v1/agents/rollout/lease", bearer: oldToken, anonymous: true, body: rolloutCaps()}); response.Code != 401 {
		t.Fatal(response.Code)
	}
	rolloutResponse[any](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/agents/rollout/lease", bearer: fresh.Plaintext, anonymous: true, body: rolloutCaps()}))
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET absolute_expires_at=? WHERE id=?", f.now.Add(-time.Second), f.owner.Principal.Session.ID); err != nil {
		t.Fatal(err)
	}
	if response := f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"}); response.Code != 401 {
		t.Fatal(response.Code)
	}
}

func TestRolloutUploadRechecksSessionAfterReadingBody(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	release := rolloutResponse[agentrollout.Release](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: rolloutTestOrigin, body: f.signed}))
	request := f.request(t, rolloutCall{method: "PUT", path: "/api/v1/admin/agent-releases/" + release.ID + "/artifacts/linux-amd64", origin: rolloutTestOrigin, body: f.artifact, headers: map[string]string{"Content-Type": "application/octet-stream"}})
	request.Body = &rolloutAuthorityReader{Reader: bytes.NewReader(f.artifact), change: func() {
		if _, err := f.db.DB.Exec("UPDATE admin_sessions SET revoked_at=? WHERE id=?", f.now, f.owner.Principal.Session.ID); err != nil {
			t.Error(err)
		}
	}}
	response := httptest.NewRecorder()
	f.router.ServeHandler().ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal(response.Code, response.Body.String())
	}
	var state string
	if err := f.db.DB.QueryRow("SELECT state FROM agent_release_artifacts WHERE release_id=?", release.ID).Scan(&state); err != nil || state != "missing" {
		t.Fatal(state, err)
	}
	var succeeded, failed int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='release.artifact.import' AND outcome='success'").Scan(&succeeded); err != nil || succeeded != 0 {
		t.Fatal(succeeded, err)
	}
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='release.artifact.import' AND outcome='failure'").Scan(&failed); err != nil || failed != 1 {
		t.Fatal(failed, err)
	}
}

type rolloutAuthorityReader struct {
	*bytes.Reader
	change func()
}

func (r *rolloutAuthorityReader) Read(p []byte) (int, error) {
	if r.change != nil {
		r.change()
		r.change = nil
	}
	return r.Reader.Read(p)
}

func (r *rolloutAuthorityReader) Close() error { return nil }

func TestRolloutLoggingAndRecoveryNeverRecordCredentialsOrBodies(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f.verifier.panicValue = "private-nonce-authorization-body"
	request := f.request(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: rolloutTestOrigin, body: f.signed, bearer: "private-bearer"})
	request.Header.Set("X-Beat-Rollout-Nonce", "private-header-nonce")
	response := httptest.NewRecorder()
	middleware.Logging(middleware.Recovery(f.router.ServeHandler())).ServeHTTP(response, request)
	if response.Code != 500 {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, secret := range []string{f.verifier.panicValue, "private-bearer", "private-header-nonce", "fixture-bundle", f.owner.Token} {
		if strings.Contains(log.String(), secret) || strings.Contains(response.Body.String(), secret) {
			t.Fatal("sensitive rollout content escaped")
		}
	}
}

func TestRolloutAdmissionRateAndDuplicateAuthorizationRejection(t *testing.T) {
	f := newRolloutRouteFixture(t, false)
	_, token := f.node(t)
	request := f.request(t, rolloutCall{method: "POST", path: "/api/v1/agents/rollout/lease", bearer: token, anonymous: true, body: rolloutCaps()})
	request.Header.Add("Authorization", "Bearer extra")
	response := httptest.NewRecorder()
	f.router.ServeHandler().ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal(response.Code)
	}
	f.router.rolloutRequests.owners.policy.burst = 1
	f.router.rolloutRequests.owners.policy.rate = 0
	rejected := false
	for range 2 {
		response := f.call(t, rolloutCall{method: "GET", path: "/api/v1/admin/agent-rollout-status"})
		if response.Code == 429 {
			if response.Header().Get("Retry-After") != "1" {
				t.Fatal(response.Header())
			}
			rejected = true
			break
		}
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
	}
	if !rejected {
		t.Fatal("owner request rate was not bounded")
	}
}

func TestRolloutAgentRevocationDuringBodyReadCannotAcquireLease(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	_, token := f.node(t)
	request := f.request(t, rolloutCall{method: "POST", path: "/api/v1/agents/rollout/lease", bearer: token, anonymous: true, body: rolloutCaps()})
	payload, err := json.Marshal(rolloutCaps())
	if err != nil {
		t.Fatal(err)
	}
	request.Body = &rolloutAuthorityReader{Reader: bytes.NewReader(payload), change: func() {
		if _, err := f.db.DB.Exec("UPDATE nodes SET agent_token_revoked_at=?", f.now); err != nil {
			t.Error(err)
		}
	}}
	response := httptest.NewRecorder()
	f.router.ServeHandler().ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestRolloutCentralUploadConcurrencyUsesOneSharedHandler(t *testing.T) {
	f := newRolloutRouteFixture(t, true)
	release := rolloutResponse[agentrollout.Release](t, f.call(t, rolloutCall{method: "POST", path: "/api/v1/admin/agent-releases/import", origin: rolloutTestOrigin, body: f.signed}))
	path := "/api/v1/admin/agent-releases/" + release.ID + "/artifacts/linux-amd64"
	gate := make(chan struct{})
	entered := make(chan struct{}, 4)
	done := make(chan int, 4)
	var once sync.Once
	var wait sync.WaitGroup
	open := func() { once.Do(func() { close(gate) }) }
	defer func() { open(); wait.Wait() }()
	for range 4 {
		request := f.request(t, rolloutCall{method: "PUT", path: path, origin: rolloutTestOrigin, body: f.artifact, headers: map[string]string{"Content-Type": "application/octet-stream"}})
		request.Body = &rolloutAuthorityReader{Reader: bytes.NewReader(f.artifact), change: func() { entered <- struct{}{}; <-gate }}
		wait.Go(func() {
			response := httptest.NewRecorder()
			f.router.ServeHandler().ServeHTTP(response, request)
			done <- response.Code
		})
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("uploads did not enter bounded streaming stage")
		}
	}
	refused := f.call(t, rolloutCall{method: "PUT", path: path, origin: rolloutTestOrigin, body: f.artifact, headers: map[string]string{"Content-Type": "application/octet-stream"}})
	if refused.Code != 503 || refused.Header().Get("Retry-After") != "1" {
		t.Fatal(refused.Code, refused.Header())
	}
	open()
	wait.Wait()
	for range 4 {
		if code := <-done; code != 200 {
			t.Fatal(code)
		}
	}
}
