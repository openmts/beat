//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type callbackGateway struct {
	*fakeGateway
	beforeLease, beforeStart func()
}

func (gateway *callbackGateway) Lease(ctx context.Context, policy protocol.Policy) (*protocol.Lease, error) {
	lease, err := gateway.fakeGateway.Lease(ctx, policy)
	if gateway.beforeLease != nil {
		gateway.beforeLease()
	}
	return lease, err
}

func (gateway *callbackGateway) Start(ctx context.Context, lease protocol.Lease) error {
	if err := gateway.fakeGateway.Start(ctx, lease); err != nil {
		return err
	}
	if gateway.beforeStart != nil {
		gateway.beforeStart()
	}
	return nil
}

func TestStartAuthorizationCannotCrossALocalJournalWriteFailure(t *testing.T) {
	lease := testLease("touch forbidden")
	config := testConfig(t)
	gateway := &callbackGateway{fakeGateway: &fakeGateway{lease: &lease}}
	runner, err := NewRunner(RunnerConfig{Local: config, Gateway: gateway})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	gateway.beforeStart = func() { blockJournalPath(t, runner, lease.AttemptID) }
	if err := runner.RunOnce(t.Context()); !errors.Is(err, ErrStorage) {
		t.Fatalf("journal failure ignored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(config.FileRoots[0].Path, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("executed without durable authorization journal")
	}
}

func TestLostCompletionStorageDoesNotRepeatASideEffect(t *testing.T) {
	lease := testLease("printf x >> marker")
	gateway := &fakeGateway{lease: &lease}
	config := testConfig(t)
	var runner *Runner
	var err error
	runner, err = NewRunner(RunnerConfig{Local: config, Gateway: gateway, OnEvent: func(event Event) {
		if event.State == "running" {
			blockJournalPath(t, runner, lease.AttemptID)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	if err := runner.RunOnce(t.Context()); !errors.Is(err, ErrStorage) {
		t.Fatal("completion storage failure ignored")
	}
	journalPath := filepath.Join(runner.journals.directory(lease.AttemptID), "journal.json")
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(journalPath+".retained", journalPath); err != nil {
		t.Fatal(err)
	}
	if err := runner.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gateway.results[0].State != "interrupted" {
		t.Fatal("uncertain effect was reported as safely retryable")
	}
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(config.FileRoots[0].Path, "marker"))
	if err != nil || string(marker) != "x" {
		t.Fatal("lost completion caused duplicate execution")
	}
}

func blockJournalPath(t *testing.T, runner *Runner, id string) {
	t.Helper()
	path := filepath.Join(runner.journals.directory(id), "journal.json")
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionRejectsFilesystemObstructionAndWrongNonce(t *testing.T) {
	lease := testLease("true")
	gateway := &callbackGateway{fakeGateway: &fakeGateway{lease: &lease}}
	config := testConfig(t)
	runner, err := NewRunner(RunnerConfig{Local: config, Gateway: gateway})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	gateway.beforeLease = func() {
		if err := os.WriteFile(runner.journals.directory(lease.AttemptID), []byte("obstruction"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.RunOnce(t.Context()); err == nil {
		t.Fatal("obstructed journal was accepted")
	}
	if gateway.starts != 0 {
		t.Fatal("server start requested before lease storage")
	}
	if err := os.Remove(runner.journals.directory(lease.AttemptID)); err != nil {
		t.Fatal(err)
	}
	if err := runner.journals.save(journal{Version: 1, State: "leased", Lease: lease}); err != nil {
		t.Fatal(err)
	}
	different := lease
	different.Nonce = "00" + lease.Nonce[2:]
	if err := runner.acceptLease(t.Context(), different); !errors.Is(err, ErrPolicy) {
		t.Fatal("cross-nonce journal reuse accepted")
	}
	if !runner.admit(lease.AttemptID) {
		t.Fatal("admission unexpectedly unavailable")
	}
	if err := runner.acceptLease(t.Context(), lease); !errors.Is(err, ErrPolicy) {
		t.Fatal("concurrent duplicate admitted")
	}
	if err := runner.recoverRecord(t.Context(), journal{Version: 1, State: "running", Lease: lease}); err != nil {
		t.Fatal(err)
	}
	runner.release(lease.AttemptID)
}

func TestRunnerCloseStopsPollingWithoutExternalCancellation(t *testing.T) {
	runner, _ := testRunner(t, &fakeGateway{})
	runner.poll = time.Millisecond
	done := make(chan error, 1)
	go func() { done <- runner.Run(context.Background()) }()
	time.Sleep(5 * time.Millisecond)
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
