package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestRolloutStartupDisabledDoesNotReadTrustOrCreateCache(t *testing.T) {
	f := newCommercialFixture(t)
	path := filepath.Join(f.directory, "disabled.json")
	writeRolloutStartup(t, path, rolloutConfiguration{SchemaVersion: 1, Sigstore: rolloutSignatureTrust{TrustedRootFile: "/must-not-read", Executable: "/must-not-execute"}})
	if err := f.services.initializeAgentRollouts(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	if f.services.agentRollouts == nil || f.services.agentRollouts.Enabled() {
		t.Fatal("disabled module unavailable or unexpectedly enabled")
	}
	if _, err := os.Stat(filepath.Join(f.directory, "agent-release-cache")); !os.IsNotExist(err) {
		t.Fatal("disabled module created a cache", err)
	}
	var count int
	if err := f.stores.sqlite.DB.QueryRow("SELECT COUNT(*) FROM agent_release_trust").Scan(&count); err != nil || count != 0 {
		t.Fatal("disabled module wrote trust", count, err)
	}
}

func TestRolloutStartupLoadsOutOfBandTrustAndChecksCurrentState(t *testing.T) {
	f := newCommercialFixture(t)
	path, config := rolloutStartupFixture(t, f.directory)
	writeRolloutStartup(t, path, config)
	if err := f.services.initializeAgentRollouts(t.Context(), f.stores, f.directory); err != nil {
		t.Fatal(err)
	}
	monitor := rolloutMonitoring(f.services)
	if monitor == nil || !monitor.Enabled() || monitor.Ready(t.Context()) != nil {
		t.Fatal("configured rollout did not become ready")
	}
	operations := &operability{agentRollouts: monitor}
	if check := operations.agentRolloutCheck(t.Context()); check.Status != "ok" {
		t.Fatal(check)
	}
	if _, err := f.stores.sqlite.DB.Exec("UPDATE agent_release_trust SET revoked_at=?", model.NowUTC()); err != nil {
		t.Fatal(err)
	}
	if check := operations.agentRolloutCheck(t.Context()); check.Status != "error" || check.Detail != "unavailable" {
		t.Fatal("revocation did not affect readiness", check)
	}
}

func TestRolloutStartupRejectsInvalidEnabledConfiguration(t *testing.T) {
	for _, kind := range []string{"missing-config", "missing-root", "invalid-root", "invalid-verifier", "invalid-keys", "cancelled", "closed-database"} {
		t.Run(kind, func(t *testing.T) {
			f := newCommercialFixture(t)
			path, config := rolloutStartupFixture(t, f.directory)
			ctx := t.Context()
			switch kind {
			case "missing-root":
				config.Sigstore.TrustedRootFile += ".missing"
			case "invalid-root":
				if err := os.WriteFile(config.Sigstore.TrustedRootFile, []byte("invalid JSON"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "invalid-verifier":
				config.Sigstore.Executable += ".missing"
			case "invalid-keys":
				config.Keys = nil
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "closed-database":
				if err := f.stores.sqlite.Close(); err != nil {
					t.Fatal(err)
				}
			}
			writeRolloutStartup(t, path, config)
			if kind == "missing-config" {
				t.Setenv("BEAT_AGENT_ROLLOUT_CONFIG", path+".missing")
			}
			if err := f.services.initializeAgentRollouts(ctx, f.stores, f.directory); err == nil {
				t.Fatal("invalid configured rollout started")
			}
		})
	}
}
