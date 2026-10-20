//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type fakeGateway struct {
	mu                    sync.Mutex
	lease                 *protocol.Lease
	startErr, completeErr error
	starts, completions   int
	results               []protocol.Result
	cancel                bool
}

func (gateway *fakeGateway) Lease(context.Context, protocol.Policy) (*protocol.Lease, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.lease, nil
}

func (gateway *fakeGateway) Start(context.Context, protocol.Lease) error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.starts++
	return gateway.startErr
}

func (gateway *fakeGateway) Heartbeat(context.Context, protocol.Lease) (bool, error) {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.cancel, nil
}

func (gateway *fakeGateway) Complete(_ context.Context, input protocol.Completion) error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	gateway.completions++
	gateway.results = append(gateway.results, input.Result)
	return gateway.completeErr
}

func testRunner(t *testing.T, gateway *fakeGateway) (*Runner, Config) {
	t.Helper()
	config := testConfig(t)
	runner, err := NewRunner(RunnerConfig{Local: config, Gateway: gateway})
	if err != nil {
		t.Fatal(err)
	}
	runner.heartbeat = 10 * time.Millisecond
	t.Cleanup(func() { _ = runner.Close() })
	return runner, config
}

func TestStartRequiresDurableServerAuthorizationAndDeduplicatesAcknowledgedLease(t *testing.T) {
	lease := testLease("printf x >> marker")
	gateway := &fakeGateway{lease: &lease, startErr: ErrTransport}
	runner, config := testRunner(t, gateway)
	if err := runner.RunOnce(t.Context()); !errors.Is(err, ErrTransport) {
		t.Fatal("start rejection ignored")
	}
	marker := filepath.Join(config.FileRoots[0].Path, "marker")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("started before durable authorization")
	}
	gateway.startErr = nil
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "x" || gateway.starts != 2 {
		t.Fatal("attempt executed more than once")
	}
	records, err := runner.journals.list()
	if err != nil || len(records) != 0 {
		t.Fatal("acknowledged output retained")
	}
}

func TestResultRetryPreservesExactCompletionAndDoesNotExecuteAgain(t *testing.T) {
	lease := testLease("printf x >> marker; printf retained")
	gateway := &fakeGateway{lease: &lease, completeErr: ErrTransport}
	runner, config := testRunner(t, gateway)
	if err := runner.RunOnce(t.Context()); !errors.Is(err, ErrTransport) {
		t.Fatal("missing completion retry")
	}
	gateway.completeErr = nil
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gateway.starts != 1 || len(gateway.results) != 2 || !reflect.DeepEqual(gateway.results[0], gateway.results[1]) {
		t.Fatal("completion was not immutable and idempotent")
	}
	content, err := os.ReadFile(filepath.Join(config.FileRoots[0].Path, "marker"))
	if err != nil || string(content) != "x" {
		t.Fatal("retry executed again")
	}
}

func TestRestartNeverRelaunchesAuthorizedOrRunningJournals(t *testing.T) {
	for _, state := range []string{"authorized", "running"} {
		t.Run(state, func(t *testing.T) {
			gateway := &fakeGateway{}
			runner, config := testRunner(t, gateway)
			lease := testLease("touch duplicated")
			lease.Payload.Command = nil
			if err := runner.journals.save(journal{Version: 1, State: state, Lease: lease}); err != nil {
				t.Fatal(err)
			}
			if err := runner.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if gateway.starts != 0 || len(gateway.results) != 1 || gateway.results[0].State != "interrupted" {
				t.Fatal("restart relaunched or misreported interrupted task")
			}
			if _, err := os.Stat(filepath.Join(config.FileRoots[0].Path, "duplicated")); !os.IsNotExist(err) {
				t.Fatal("script relaunched")
			}
		})
	}
}

func TestServerCancellationTerminatesCommand(t *testing.T) {
	lease := testLease("sleep 60")
	gateway := &fakeGateway{lease: &lease, cancel: true}
	runner, _ := testRunner(t, gateway)
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(gateway.results) != 1 || gateway.results[0].State != "canceled" {
		t.Fatal("cancellation not acknowledged after termination")
	}
}

func TestDisabledAndShutdownRunner(t *testing.T) {
	runner, err := NewRunner(RunnerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if runner.Capabilities().Enabled {
		t.Fatal("operations enabled by default")
	}
	active, _ := testRunner(t, &fakeGateway{})
	ctx, cancel := context.WithCancel(t.Context())
	active.poll = time.Millisecond
	done := make(chan error, 1)
	go func() { done <- active.Run(ctx) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestExpiredPersistedLeaseCanRecoverLostStartAcknowledgement(t *testing.T) {
	gateway := &fakeGateway{}
	runner, _ := testRunner(t, gateway)
	lease := testLease("true")
	lease.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
	if err := runner.journals.save(journal{Version: 1, State: "leased", Lease: lease}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gateway.starts != 1 || len(gateway.results) != 1 || gateway.results[0].State != "succeeded" {
		t.Fatal("lost authorization acknowledgement not reconciled")
	}
}
