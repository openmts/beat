package agentdiagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

func TestRunOnceWithoutLeaseOnlyAdvertises(t *testing.T) {
	gateway := &fakeGateway{}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	state := gateway.snapshot()
	if state.advertised != 1 || state.started != 0 || state.leases != 1 {
		t.Fatalf("gateway state = %+v", state)
	}
}

func TestRunOnceExecutesLeaseInBoundedBatches(t *testing.T) {
	gateway := &fakeGateway{lease: benchmarkLease("8.8.8.8")}
	plugin := fakeExecutor{kind: "mtr", result: terminalOutcome(model.DiagnosticStateSucceeded, ""),
		frames: []batch{{Samples: hopSamples(300), Hops: []diagnosticprotocol.HopIdentity{{Index: 1, Reached: true}}}}}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": plugin})
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	state := gateway.snapshot()
	if state.started != 1 || state.hearts != 0 || len(state.progress) != 3 || len(state.completions) != 1 {
		t.Fatalf("gateway state = %+v", state)
	}
	if len(state.progress[0].Samples) != maximumSamplesPerBatch || len(state.progress[2].Samples) != 300-2*maximumSamplesPerBatch {
		t.Fatalf("progress batches = %d/%d", len(state.progress[0].Samples), len(state.progress[2].Samples))
	}
	if len(state.progress[0].Hops) != 1 || len(state.progress[1].Hops) != 0 {
		t.Fatal("hop identity must travel with the first batch only")
	}
	for index, frame := range state.progress {
		if frame.Sequence != int64(index+1) || frame.JobID != "job-1" || frame.Nonce != "nonce" {
			t.Fatalf("progress frame %d = %+v", index, frame)
		}
	}
	completion := state.completions[0]
	if completion.State != model.DiagnosticStateSucceeded || completion.Attempt != 1 {
		t.Fatalf("completion = %+v", completion)
	}
	assertNoPendingJournal(t, runner)
	assertStatus(t, runner, 1, 0)
}

func TestRunOnceRejectsUnapprovedDestinations(t *testing.T) {
	for _, destination := range []string{"1.1.1.1", "127.0.0.1", "not-an-address"} {
		gateway := &fakeGateway{lease: benchmarkLease(destination)}
		runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{
			kind: "mtr", result: terminalOutcome(model.DiagnosticStateSucceeded, "")}})
		if err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce(%s) error = %v", destination, err)
		}
		state := gateway.snapshot()
		if state.started != 1 || len(state.progress) != 0 || len(state.completions) != 1 {
			t.Fatalf("gateway state for %s = %+v", destination, state)
		}
		if state.completions[0].State != model.DiagnosticStateFailed || state.completions[0].FailureCode != "local_policy" {
			t.Fatalf("completion for %s = %+v", destination, state.completions[0])
		}
		assertNoPendingJournal(t, runner)
	}
}

func TestRunOnceRejectsUnsupportedKindWithoutExecution(t *testing.T) {
	lease := benchmarkLease("8.8.8.8")
	lease.Payload.Kind = model.DiagnosticKindIperf3
	gateway := &fakeGateway{lease: lease}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	state := gateway.snapshot()
	if len(state.completions) != 1 || state.completions[0].State != model.DiagnosticStateFailed {
		t.Fatalf("gateway state = %+v", state)
	}
}

func TestRunOnceCancelsExecutionOnHeartbeat(t *testing.T) {
	gateway := &fakeGateway{lease: benchmarkLease("8.8.8.8"), cancel: true}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": blockingExecutor{kind: "mtr"}})
	runner.settings.heartbeat = time.Millisecond
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	state := gateway.snapshot()
	if state.hearts < 1 || len(state.completions) != 1 {
		t.Fatalf("gateway state = %+v", state)
	}
	if state.completions[0].State != model.DiagnosticStateCancelled {
		t.Fatalf("completion = %+v", state.completions[0])
	}
	assertNoPendingJournal(t, runner)
	assertStatus(t, runner, 0, 1)
}

func TestRunOnceReportsUnknownWhenProgressFails(t *testing.T) {
	gateway := &fakeGateway{lease: benchmarkLease("8.8.8.8"), progressErr: ErrTransport}
	plugin := fakeExecutor{kind: "mtr", frames: []batch{{Samples: hopSamples(4)}}}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": plugin})
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	state := gateway.snapshot()
	if len(state.completions) != 1 || state.completions[0].State != model.DiagnosticStateUnknown ||
		state.completions[0].FailureCode != "report_failed" {
		t.Fatalf("gateway state = %+v", state)
	}
}

func TestRunOnceKeepsJournalWhenCompletionFails(t *testing.T) {
	gateway := &fakeGateway{lease: benchmarkLease("8.8.8.8"), completeErr: ErrTransport}
	plugin := fakeExecutor{kind: "mtr", result: terminalOutcome(model.DiagnosticStateSucceeded, "")}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": plugin})
	if err := runner.RunOnce(context.Background()); !errors.Is(err, ErrTransport) {
		t.Fatalf("RunOnce error = %v, want ErrTransport", err)
	}
	entries, err := runner.journals.pending()
	if err != nil || len(entries) != 1 || entries[0].State != journalStateStarted {
		t.Fatalf("pending journal = %+v (%v)", entries, err)
	}
}

func TestAcceptSkipsAlreadyRecordedAttempt(t *testing.T) {
	gateway := &fakeGateway{lease: benchmarkLease("8.8.8.8")}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.journals.save(journalEntry{JobID: "job-1", Nonce: "nonce", Attempt: 1, State: journalStateLeased}); err != nil {
		t.Fatalf("journal save error = %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce error = %v", err)
	}
	if state := gateway.snapshot(); state.started != 0 || len(state.completions) != 0 {
		t.Fatalf("gateway state = %+v", state)
	}
}

func TestRecoverMarksLeftoverAttemptsUnknown(t *testing.T) {
	gateway := &fakeGateway{}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.journals.save(journalEntry{JobID: "job-1", Nonce: "nonce", Attempt: 2, State: journalStateStarted}); err != nil {
		t.Fatalf("journal save error = %v", err)
	}
	if err := runner.recover(context.Background()); err != nil {
		t.Fatalf("recover error = %v", err)
	}
	state := gateway.snapshot()
	if len(state.completions) != 1 || state.completions[0].State != model.DiagnosticStateUnknown ||
		state.completions[0].FailureCode != "agent_restarted" || state.completions[0].Attempt != 2 {
		t.Fatalf("gateway state = %+v", state)
	}
	assertNoPendingJournal(t, runner)
}

func TestRecoverDropsEntriesTheServerRejects(t *testing.T) {
	gateway := &fakeGateway{completeErr: ErrPolicy}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	if err := runner.journals.save(journalEntry{JobID: "job-1", Nonce: "nonce", Attempt: 1, State: journalStateLeased}); err != nil {
		t.Fatalf("journal save error = %v", err)
	}
	if err := runner.recover(context.Background()); err != nil {
		t.Fatalf("recover error = %v", err)
	}
	assertNoPendingJournal(t, runner)
}

func TestNewRunnerRequiresGatewayAndDisabledRunnerStaysIdle(t *testing.T) {
	config := validConfig(t)
	if _, err := NewRunner(RunnerConfig{Local: config}); !errors.Is(err, ErrTransport) {
		t.Fatalf("NewRunner error = %v, want ErrTransport", err)
	}
	disabled, err := NewRunner(RunnerConfig{Local: Config{}})
	if err != nil {
		t.Fatalf("NewRunner(disabled) error = %v", err)
	}
	if status := disabled.Status(); status.Enabled || status.Ready == false {
		t.Fatalf("disabled status = %+v", status)
	}
	if err := disabled.Run(context.Background()); err != nil {
		t.Fatalf("disabled Run error = %v", err)
	}
	if err := disabled.RunOnce(context.Background()); err != nil {
		t.Fatalf("disabled RunOnce error = %v", err)
	}
	if err := disabled.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
}

func TestRunPollsUntilContextCancels(t *testing.T) {
	gateway := &fakeGateway{}
	runner := diagnosticRunner(t, gateway, map[string]executor{"mtr": fakeExecutor{kind: "mtr"}})
	runner.settings.poll = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- runner.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for gateway.snapshot().advertised == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runner never advertised its capability")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-finished; err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
}

func assertNoPendingJournal(t *testing.T, runner *Runner) {
	t.Helper()
	entries, err := runner.journals.pending()
	if err != nil || len(entries) != 0 {
		t.Fatalf("pending journal = %+v (%v)", entries, err)
	}
}

func assertStatus(t *testing.T, runner *Runner, completed, failed int64) {
	t.Helper()
	status := runner.Status()
	if status.Completed != completed || status.Failed != failed || status.Active != 0 {
		t.Fatalf("status = %+v", status)
	}
}
