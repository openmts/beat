//go:build linux

package agentupdate_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/agentupdate"
)

func (f *transportFixture) clients(t *testing.T) (*agentupdate.ControlClient, *agentupdate.Updater) {
	t.Helper()
	settings := agentupdate.HTTPConfig{ServerURL: f.server.URL, AgentToken: f.token, TLS: f.tls}
	client, err := agentupdate.NewControlClient(settings)
	transportMust(t, err)
	t.Cleanup(client.Close)
	fetcher, err := agentupdate.NewHTTPFetcher(settings)
	transportMust(t, err)
	t.Cleanup(fetcher.Close)
	f.config.Fetcher = fetcher
	updater, err := agentupdate.New(f.config)
	transportMust(t, err)
	return client, updater
}

func (f *transportFixture) lease(t *testing.T, client *agentupdate.ControlClient, updater *agentupdate.Updater) *protocol.Assignment {
	t.Helper()
	caps, err := updater.Capabilities()
	transportMust(t, err)
	offer, err := client.Lease(t.Context(), caps)
	transportMust(t, err)
	if offer == nil || offer.Action != protocol.AssignmentUpgrade || offer.ServerSchema != f.schema || offer.NodeID != f.node.ID {
		t.Fatalf("unexpected authenticated offer: %+v", offer)
	}
	duplicate, err := client.Lease(t.Context(), caps)
	transportMust(t, err)
	if duplicate != nil {
		t.Fatal("lost-response retry invented another nonce")
	}
	return offer
}

func TestTLSRolloutTransportUsesRealAuthManifestRangeAndACK(t *testing.T) {
	f := newTransportFixture(t)
	client, updater := f.clients(t)
	caps, err := updater.Capabilities()
	transportMust(t, err)
	empty, err := client.Lease(t.Context(), caps)
	transportMust(t, err)
	if empty != nil {
		t.Fatal("unexpected initial assignment")
	}
	f.publish(t)
	offer := f.lease(t, client, updater)
	bad := *offer
	bad.Nonce = strings.Repeat("f", 64)
	_, err = f.config.Fetcher.Fetch(t.Context(), agentupdate.FetchRequest{Assignment: bad, Length: 1})
	if !errors.Is(err, agentupdate.ErrDownload) {
		t.Fatal("bad nonce accepted", err)
	}
	status, err := updater.Apply(t.Context(), *offer)
	transportMust(t, err)
	if status.Phase != "published" || f.manager.calls.Load() != 1 {
		t.Fatal(status)
	}
	current, err := os.ReadFile(f.config.Executable)
	transportMust(t, err)
	if !bytes.Equal(f.next, current) {
		t.Fatal("signed artifact was not published")
	}
	f.checkRanges(t, *offer)
	f.config.Installed, err = agentupdate.ResolveInstalled(t.Context(), f.config)
	transportMust(t, err)
	restarted, err := agentupdate.New(f.config)
	transportMust(t, err)
	f.ackHealth(t, client, restarted)
	_, targets, err := f.service.Detail(t.Context(), f.owner, f.rollout.ID)
	transportMust(t, err)
	if len(targets) != 1 || targets[0].State != "healthy" {
		t.Fatalf("server health gate did not complete: %+v", targets)
	}
	_, err = f.nodes.RevokeAgentToken(t.Context(), f.node.ID, f.now())
	transportMust(t, err)
	_, err = client.Lease(t.Context(), caps)
	if !errors.Is(err, agentupdate.ErrDownload) {
		t.Fatal("revoked credential accepted", err)
	}
}

func (f *transportFixture) checkRanges(t *testing.T, offer protocol.Assignment) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	ranges := []string{}
	for _, request := range f.requests {
		if !strings.Contains(request.path, "/artifacts/") {
			continue
		}
		if request.path != "/api/v1/agents/rollout/artifacts/"+offer.ID+"/"+offer.Artifact.ID {
			t.Fatal("wrong artifact endpoint")
		}
		if request.nonce == offer.Nonce {
			ranges = append(ranges, request.offset)
		}
	}
	if len(ranges) != 2 || ranges[0] != "bytes=0-4194303" || ranges[1] != "bytes=4194304-4194815" {
		t.Fatal("bounded ranges changed", ranges)
	}
}

func TestTLSIndependentSupervisorReportsWatchdogRollback(t *testing.T) {
	f := newTransportFixture(t)
	f.config.StartupTimeout = time.Second
	f.config.ObservationWindow = time.Second
	client, updater := f.clients(t)
	f.publish(t)
	offer := f.lease(t, client, updater)
	status, err := updater.Apply(t.Context(), *offer)
	transportMust(t, err)
	if status.Phase != "published" {
		t.Fatal(status)
	}
	f.advance(3 * time.Second)
	supervisor := agentupdate.Supervisor{Interval: time.Millisecond, Build: func(context.Context) (*agentupdate.Updater, error) { return updater, nil }}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx) }()
	select {
	case <-f.observations:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not report rollback")
	}
	cancel()
	transportMust(t, <-done)
	current, err := os.ReadFile(f.config.Executable)
	transportMust(t, err)
	if !bytes.Equal(f.old, current) || f.manager.calls.Load() != 2 {
		t.Fatal("independent helper did not restore old entrypoint")
	}
	rollout, targets, err := f.service.Detail(t.Context(), f.owner, f.rollout.ID)
	transportMust(t, err)
	if rollout.State != "paused" || len(targets) != 1 || targets[0].State != "failed" || targets[0].Reason != "agent_rolled_back" {
		t.Fatal(rollout, targets)
	}
}
