package agentinstall

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
)

func TestEnrollRejectsInvalidOptions(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	options := fixture.options()
	options.ServerURL = "ftp://beat.example.com"
	if _, err := fixture.installer.Enroll(context.Background(), options); err == nil {
		t.Fatal("expected invalid server URL to fail")
	}
	options = fixture.options()
	options.GrantSecret = "not-a-grant-secret"
	if _, err := fixture.installer.Enroll(context.Background(), options); err == nil {
		t.Fatal("expected invalid grant secret to fail")
	}
}

func TestEnrollSurfacesServerRejection(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	unregistered, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatal(err)
	}
	options := fixture.options()
	options.GrantSecret = unregistered.Plaintext
	_, err = fixture.installer.Enroll(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "server rejected enrollment") {
		t.Fatalf("expected server rejection, got %v", err)
	}
}

func TestEnrollRejectsStoredIdentityDamage(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	mustMkdir(t, fixture.installer.paths.stateDir)
	path := filepath.Join(fixture.installer.paths.stateDir, "identity.json")
	for _, body := range []string{
		`{"version":9,"public_key":"AA","private_key":"AA"}`,
		`{"version":1,"public_key":"AA","private_key":"AA"}`,
		`{"version":1,`,
	} {
		if err := os.WriteFile(path, []byte(body), privateFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, privateFile); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.installer.Enroll(context.Background(), fixture.options()); err == nil {
			t.Fatalf("expected stored identity %s to be rejected", body)
		}
	}
}

func TestEnrollRejectsReusedClaimedRequest(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	if _, err := fixture.installer.Enroll(context.Background(), fixture.options()); err != nil {
		t.Fatalf("initial enroll: %v", err)
	}
	path := filepath.Join(fixture.installer.paths.stateDir, grantStateFile)
	var state grantState
	if err := decodeJSON(mustOpen(t, path), &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	state.Result = nil
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, privateFile); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.installer.Enroll(context.Background(), fixture.options()); err == nil ||
		!strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("expected claimed request to be reported, got %v", err)
	}
}

func TestCheckGrantConfigMatchesOrRefusesExistingFile(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	mustMkdir(t, filepath.Dir(fixture.installer.paths.config))
	config := grantConfig(grantClaim{ServerURL: discoveryOrigin, AgentToken: "beat_agent_v1_token",
		NodeName: "lab-1", Host: "192.0.2.10", Port: 22, ReportInterval: "5s"})
	if err := fixture.installer.checkGrantConfig(config); err != nil {
		t.Fatalf("missing config should be accepted: %v", err)
	}
	if err := fixture.installer.saveJSON(fixture.installer.paths.config, config); err != nil {
		t.Fatal(err)
	}
	if err := fixture.installer.checkGrantConfig(config); err != nil {
		t.Fatalf("identical config should be accepted: %v", err)
	}
	other := config
	other.AgentToken = "beat_agent_v1_other"
	if err := fixture.installer.checkGrantConfig(other); err == nil {
		t.Fatal("conflicting config was overwritten")
	}
}

func TestEnrollRejectsMismatchedServerChallenge(t *testing.T) {
	fixture := newDiscoveryInstaller(t, agentenrollment.ApprovalAutomatic)
	fixture.installer.client = &http.Client{Transport: routerTransport{handler: http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 201, "data": map[string]any{
				"challenge_id": "id", "challenge": encodeAgentBytes(make([]byte, agentenrollment.ChallengeSize)),
				"origin": "https://beat.example.com", "expires_at": time.Now().Add(time.Minute).Format(time.RFC3339),
				"protocol_version": "other-protocol"}})
		})}}
	if _, err := fixture.installer.Enroll(context.Background(), fixture.options()); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected protocol mismatch, got %v", err)
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
