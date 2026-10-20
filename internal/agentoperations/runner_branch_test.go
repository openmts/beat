//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestStartDropsRevokedLeaseWithoutExecuting(t *testing.T) {
	lease := testLease("touch revoked")
	gateway := &fakeGateway{lease: &lease, startErr: ErrLeaseRejected}
	runner, config := testRunner(t, gateway)
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatalf("revoked lease error = %v", err)
	}
	records, err := runner.journals.list()
	if err != nil || len(records) != 0 {
		t.Fatalf("revoked lease retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.FileRoots[0].Path, "revoked")); !os.IsNotExist(err) {
		t.Fatal("revoked lease executed")
	}
}

func TestPersistedLeaseWithUnauthorizedRootIsRejected(t *testing.T) {
	runner, _ := testRunner(t, &fakeGateway{})
	lease := testLease("touch unauthorized")
	lease.Payload.Limits.RunAsRoot = os.Geteuid() != 0
	if err := runner.journals.save(journal{Version: 1, State: "leased", Lease: lease}); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(t.Context()); err == nil {
		t.Fatal("unauthorized persisted lease started")
	}
}

func TestRunOnceRejectsUnauthorizedGatewayLease(t *testing.T) {
	lease := testLease("")
	gateway := &fakeGateway{lease: &lease}
	runner, _ := testRunner(t, gateway)
	if err := runner.RunOnce(t.Context()); err == nil {
		t.Fatal("invalid gateway lease accepted")
	}
	if gateway.starts != 0 {
		t.Fatal("invalid gateway lease authorized")
	}
}

func TestCommandLeaseValidationRejectsUnauthorizedShellFamily(t *testing.T) {
	runner := &Runner{settings: settings{
		policy:   protocol.Policy{SupportedShells: []string{}},
		identity: identity{uid: 0},
	}}
	lease := testLease("id")
	lease.Payload.ShellFamily = ""
	lease.Payload.Limits.RunAsRoot = true
	if err := runner.validateCommandLease(lease); !errors.Is(err, ErrPolicy) {
		t.Fatalf("command lease error = %v", err)
	}
}

func TestResultStateMapsDisconnectedExecutionToInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	cancel(errDisconnected)
	if state := resultState(ctx, &outputCapture{}, nil); state != "interrupted" {
		t.Fatalf("state = %q", state)
	}
}

func TestEventReportsFinalizedTransferSize(t *testing.T) {
	runner, _ := testRunner(t, &fakeGateway{})
	var captured Event
	runner.onEvent = func(event Event) { captured = event }
	source := protocol.SourceInfo{Size: 42}
	runner.emit(journal{
		Version:  1,
		State:    "running",
		Lease:    testLease("true"),
		Transfer: &transferProgress{Phase: "finalized", Source: &source},
	})
	if captured.TransferredBytes != 42 || captured.State != "running" {
		t.Fatalf("event = %+v", captured)
	}
}
