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

func TestShutdownPreservesTransferProgressForFreshRunner(t *testing.T) {
	fixture := newFileFixture(t, "deploy", []byte("resumable"))
	fixture.server.configure(func(server *fileServer) { server.delayDownload = 100 * time.Millisecond })
	done := make(chan error, 1)
	go func() { done <- fixture.runner.RunOnce(t.Context()) }()
	deadline := time.Now().Add(time.Second)
	for fixture.server.snapshot().downloads == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := fixture.runner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown did not interrupt request: %v", err)
	}
	before, err := fixture.runner.journals.load("file-attempt")
	if err != nil || before.State != "running" {
		t.Fatal("shutdown destroyed resumable journal")
	}
	fixture.server.configure(func(server *fileServer) { server.delayDownload = 0 })
	reopened, err := NewRunner(RunnerConfig{Local: fixture.config, Gateway: fixture.client})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	if err := reopened.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fixture.server.snapshot().results[0].StartedAt != before.Transfer.StartedAt {
		t.Fatal("restart reset transfer duration")
	}
	writeFixturePayload(t, fixture, []byte("operator edit"))
	if err := reopened.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(fixture.root, "payload.bin"))
	if err != nil || string(content) != "operator edit" {
		t.Fatal("acknowledged lease ran again")
	}
}

func TestStatusAndEventsRemainMetadataOnly(t *testing.T) {
	lease := testLease("printf private-command-output")
	gateway := &fakeGateway{lease: &lease}
	config := testConfig(t)
	var events []Event
	var codes []string
	runner, err := NewRunner(RunnerConfig{Local: config, Gateway: gateway, OnEvent: func(event Event) { events = append(events, event) }, OnError: func(err error) { codes = append(codes, ErrorCode(err)) }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].State != "authorized" || events[1].State != "running" || events[2].State != "succeeded" || events[2].OutputBytes == 0 {
		t.Fatal("durable event sequence incomplete")
	}
	runner.observe(ErrTransport)
	runner.observe(ErrTransport)
	if len(codes) != 1 || runner.Status().Ready || runner.Status().ErrorCode != "transport_unavailable" {
		t.Fatal("error metadata was not deduplicated")
	}
	runner.observe(nil)
	if !runner.Status().Ready || runner.Status().Active != 0 {
		t.Fatal("healthy status did not recover")
	}
	for _, err := range []error{ErrPolicy, ErrStorage, ErrBoundary, ErrLeaseRejected, ErrFileIntegrity, ErrSourceChanged, ErrFileConflict, context.Canceled, context.DeadlineExceeded, errors.New("secret contents")} {
		if ErrorCode(err) == "" || ErrorCode(err) == err.Error() {
			t.Fatal("raw diagnostic escaped error-code boundary")
		}
	}
}

func TestReceiptCapacityAndAdmissionAreFailClosed(t *testing.T) {
	runner, _ := testRunner(t, &fakeGateway{})
	if !runner.admit("a") || runner.admit("a") {
		t.Fatal("duplicate attempt admitted")
	}
	runner.release("a")
	runner.settings.config.MaxConcurrency = 4
	if !runner.admitKind("f1", "deploy") || !runner.admitKind("f2", "collect") || runner.admitKind("f3", "collect") {
		t.Fatal("file concurrency limit ignored")
	}
	runner.release("f1")
	runner.release("f2")
	runner.journals.receiptCount = maximumReceipts
	if err := runner.journals.markAcknowledged("new"); !errors.Is(err, ErrStorage) {
		t.Fatal("receipt eviction could permit duplicate execution")
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	if runner.admit("closed") {
		t.Fatal("closed runner admitted work")
	}
	if err := runner.RunOnce(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal("closed runner requested a lease")
	}
}

func TestSensitiveConfigurationAndTransferEventCounts(t *testing.T) {
	path := filepath.Join(privateTestDir(t), "agent.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateConfigFile(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateConfigFile(path); err == nil {
		t.Fatal("public config accepted")
	}
	record := journal{Lease: fileLease("deploy", []byte("data")), Transfer: &transferProgress{Phase: "published"}}
	if transferredBytes(record) != 4 {
		t.Fatal("deploy byte count missing")
	}
	record.Transfer.Phase = "finalized"
	record.Transfer.Source = &protocol.SourceInfo{Size: 9}
	if transferredBytes(record) != 9 {
		t.Fatal("collect byte count missing")
	}
	record.Transfer.Phase = "pending"
	if transferredBytes(record) != 0 {
		t.Fatal("uncommitted data reported as transferred")
	}
}
