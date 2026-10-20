package agentinstall

import (
	"context"
	"testing"
)

func TestJoinInstallsPrivateConfigAndStartsService(t *testing.T) {
	fixture := newFixture(t)
	result, err := fixture.installer.Join(context.Background(), fixture.options)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if result.NodeID != fixture.result.NodeID {
		t.Fatalf("node ID: want %q, got %q", fixture.result.NodeID, result.NodeID)
	}
	fixture.assertInstalled(t)
}

func TestJoinRetryReusesEnrollment(t *testing.T) {
	fixture := newFixture(t)
	for range 2 {
		if _, err := fixture.installer.Join(context.Background(), fixture.options); err != nil {
			t.Fatalf("join: %v", err)
		}
	}
	if fixture.requests.Load() != 1 {
		t.Fatalf("join requests: want 1, got %d", fixture.requests.Load())
	}
}
