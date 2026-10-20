package agentdiagnostics

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

func TestTransportSurfacesEveryEndpointFailure(t *testing.T) {
	attempt := diagnosticprotocol.AttemptRequest{JobID: "job-1", Nonce: "nonce", Attempt: 1}
	ctx := context.Background()
	short := clientFor(t, func(*http.Request) (*http.Response, error) {
		return wireResponse(http.StatusOK, envelope(`{"digest":"short"}`)), nil
	})
	if _, err := short.Advertise(ctx, validConfig(t).Capability(capabilityStateReady)); !errors.Is(err, ErrTransport) {
		t.Fatalf("short digest error = %v", err)
	}
	failing := clientFor(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("dial failed") })
	if _, err := failing.Heartbeat(ctx, attempt); !errors.Is(err, ErrTransport) {
		t.Fatalf("heartbeat error = %v", err)
	}
	if err := failing.Complete(ctx, diagnosticprotocol.CompletionRequest{JobID: "job-1"}); !errors.Is(err, ErrTransport) {
		t.Fatalf("complete error = %v", err)
	}
	if _, err := failing.Lease(ctx, "digest"); !errors.Is(err, ErrTransport) {
		t.Fatalf("lease error = %v", err)
	}
	unknown := clientFor(t, func(*http.Request) (*http.Response, error) {
		return wireResponse(http.StatusOK, envelope(`{"sequence":1,"unexpected":true}`)), nil
	})
	if _, err := unknown.Progress(ctx, diagnosticprotocol.ProgressRequest{JobID: "job-1", Sequence: 1}); !errors.Is(err, ErrTransport) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func clientFor(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{ServerURL: "https://beat.example.com", Token: "token", Transport: transport})
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}
	return client
}

func TestExecutorsPropagateSinkAndProcessFailures(t *testing.T) {
	directory := t.TempDir()
	report := `{"report":{"hubs":[{"count":1,"Snt":10,"Recv":10,"Last":0,"Avg":1,"Best":1,"Wrst":1}]}}`
	mtr := mtrExecutor{path: writeFixtureBinary(t, "#!/bin/sh\nprintf '%s' '"+report+"'\n")}
	failing := func(batch) error { return ErrStorage }
	if _, err := mtr.Execute(context.Background(), jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolICMP,
		Probes: 10, HopLimit: 32, Directory: directory}, failing); !errors.Is(err, ErrStorage) {
		t.Fatalf("mtr sink error = %v", err)
	}
	iperf := iperf3Executor{path: writeFixtureBinary(t, "#!/bin/sh\nprintf '%s' '{\"event\":\"interval\",\"data\":{\"bits_per_second\":8,\"bytes\":1}}'\n")}
	if _, err := iperf.Execute(context.Background(), jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolTCP,
		Port: 5201, DurationSeconds: 3, ParallelStreams: 1, BitrateMbps: 1, Directory: directory}, failing); !errors.Is(err, ErrStorage) {
		t.Fatalf("iperf3 sink error = %v", err)
	}
	broken := iperf3Executor{path: writeFixtureBinary(t, "#!/bin/sh\nexit 4\n")}
	result, err := broken.Execute(context.Background(), jobSpec{Destination: "8.8.8.8", Protocol: model.DiagnosticProtocolTCP,
		Port: 5201, DurationSeconds: 3, ParallelStreams: 1, BitrateMbps: 1, Directory: directory}, func(batch) error { return nil })
	if err != nil || result.FailureCode != "exit_nonzero" {
		t.Fatalf("iperf3 failure = %+v (%v)", result, err)
	}
}

func TestRunnerSurfacesStartAndCompletionFailures(t *testing.T) {
	lease := benchmarkLease("8.8.8.8")
	gateway := &fakeGateway{lease: lease, startErr: ErrTransport}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.RunOnce(context.Background()); !errors.Is(err, ErrTransport) {
		t.Fatalf("start failure error = %v", err)
	}
	gateway = &fakeGateway{lease: lease, startErr: ErrTransport}
	runner = diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.reject(context.Background(), *lease); !errors.Is(err, ErrTransport) {
		t.Fatalf("reject failure error = %v", err)
	}
	gateway = &fakeGateway{completeErr: ErrStorage}
	runner = diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	entry := journalEntry{JobID: "job-1", Nonce: "nonce", Attempt: 1, State: journalStateStarted}
	if err := runner.journals.save(entry); err != nil {
		t.Fatalf("journal save error = %v", err)
	}
	if err := runner.recover(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatalf("recover failure error = %v", err)
	}
	if entries, err := runner.journals.pending(); err != nil || len(entries) != 1 {
		t.Fatalf("pending journal = %+v (%v)", entries, err)
	}
}

func TestStorageHelpersRejectImpossiblePaths(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writePrivateFile(filepath.Join(file, "entry.json"), []byte("{}")); !errors.Is(err, ErrStorage) {
		t.Fatalf("writePrivateFile error = %v", err)
	}
	if err := removePrivateFile(filepath.Join(file, "entry.json")); !errors.Is(err, ErrStorage) {
		t.Fatalf("removePrivateFile error = %v", err)
	}
}

func TestValidateConfigRejectsOversizedAllowlist(t *testing.T) {
	config := validConfig(t)
	entries := make([]string, 0, model.MaximumDiagnosticTargetAddresses+1)
	for range model.MaximumDiagnosticTargetAddresses + 1 {
		entries = append(entries, "8.8.8.8")
	}
	config.AllowedTargets = entries
	if err := ValidateConfig(config); !errors.Is(err, ErrPolicy) {
		t.Fatalf("oversized allowlist error = %v", err)
	}
}

func TestPendingSkipsUnusableEntries(t *testing.T) {
	log, err := openJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatalf("openJournal error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(log.directory, "nested.json"), 0o700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(log.directory, "loose.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write loose journal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(log.directory, "ignored.txt"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	entries, err := log.pending()
	if err != nil || len(entries) != 0 {
		t.Fatalf("pending = %+v (%v)", entries, err)
	}
}

func TestDirectionAndCapabilityAllowReverse(t *testing.T) {
	config := validConfig(t)
	config.AllowReverse = true
	document := config.Capability(capabilityStateReady)
	if document.Iperf3 != nil || len(config.directions()) != 2 {
		t.Fatalf("directions = %+v", config.directions())
	}
	config.Iperf3Path = "/usr/bin/iperf3"
	document = config.Capability(capabilityStateReady)
	if len(document.Iperf3.Directions) != 2 || !strings.Contains(strings.Join(document.Iperf3.Directions, ","), model.DiagnosticDirectionReverse) {
		t.Fatalf("iperf3 directions = %+v", document.Iperf3.Directions)
	}
}
