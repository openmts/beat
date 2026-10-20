//go:build linux

package agentupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDurableRestartRecovery(t *testing.T) {
	for _, point := range []string{"published_rename", "published", "rollback_rename"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			if point == "rollback_rename" {
				_, e := f.u.Apply(ctx, f.assignment)
				must(t, e)
			}
			f.crashAt(point)
			var err error
			if point == "rollback_rename" {
				_, err = f.u.Rollback(ctx)
			} else {
				_, err = f.u.Apply(ctx, f.assignment)
			}
			wantError(t, errInjected, err)
			before := f.manager.calls
			f.u.fault = nil
			_, err = f.u.Recover(ctx)
			must(t, err)
			if f.manager.calls != before+1 {
				t.Fatalf("restart missing: %d -> %d", before, f.manager.calls)
			}
			_, err = f.u.Recover(ctx)
			must(t, err)
			if f.manager.calls != before+1 {
				t.Fatal("successful restart repeated")
			}
		})
	}
}

func TestRecoverRestartFailureAndPreparedDeadline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.crashAt("published")
	_, e := f.u.Apply(ctx, f.assignment)
	wantError(t, errInjected, e)
	f.u.fault = nil
	f.manager.failures = 1
	_, e = f.u.Recover(ctx)
	wantError(t, ErrManager, e)
	_, e = f.u.Recover(ctx)
	must(t, e)
	if f.manager.calls != 2 {
		t.Fatal("failed restart not retried")
	}
	original := f.read(t).Deadline
	f.now = f.now.Add(5 * time.Second)
	_, e = f.u.Recover(ctx)
	must(t, e)
	if !f.read(t).Deadline.Equal(original) {
		t.Fatal("watchdog extended")
	}
}

func TestLeaseExpiryDuringTransfer(t *testing.T) {
	f := newFixture(t)
	f.next = testELF(int(ChunkBytes)+512, 3)
	f.resign(t, "v2.0.0")
	f.u.fault = func(point string) error {
		if point == "download_chunk" {
			f.now = f.assignment.ExpiresAt
		}
		return nil
	}
	_, e := f.u.Apply(context.Background(), f.assignment)
	wantError(t, ErrAssignment, e)
	f.checkCurrent(t, f.old)
}

func TestRecoveryJournalErrorsPropagate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.crashAt("prepared")
	_, e := f.u.Apply(ctx, f.assignment)
	wantError(t, errInjected, e)
	f.mutate(t, func(j *journal) { j.Assignment.NodeID = "different" })
	for _, call := range []func(context.Context) (Status, error){f.u.Cancel, f.u.Rollback} {
		_, e = call(ctx)
		wantError(t, ErrJournal, e)
	}
	_, e = f.u.Observe(ctx, HealthEvidence{})
	wantError(t, ErrJournal, e)
	_, e = f.u.Apply(ctx, f.assignment)
	wantError(t, ErrJournal, e)
	path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, journalFile)
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	for _, call := range []func(context.Context) (Status, error){f.u.Cancel, f.u.Rollback} {
		_, e = call(ctx)
		wantError(t, ErrJournal, e)
	}
	_, e = f.u.Observe(ctx, HealthEvidence{})
	wantError(t, ErrJournal, e)
}

func TestEntrypointConflictImmediatelyBeforeRename(t *testing.T) {
	for _, point := range []string{"publish_intent", "rollback_prepared"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			if point == "rollback_prepared" {
				_, e := f.u.Apply(ctx, f.assignment)
				must(t, e)
			}
			f.u.fault = func(p string) error {
				if p == point {
					return os.WriteFile(f.u.config.Executable, []byte("external replacement"), 0o600)
				}
				return nil
			}
			var e error
			if point == "rollback_prepared" {
				_, e = f.u.Rollback(ctx)
			} else {
				_, e = f.u.Apply(ctx, f.assignment)
			}
			wantError(t, ErrConflict, e)
			f.checkCurrent(t, []byte("external replacement"))
		})
	}
}

type countedManager struct {
	fakeManager
	automatic int
	err       error
}

func (manager *countedManager) AutomaticRestarts(context.Context) (int, error) {
	return manager.automatic, manager.err
}

func TestAutomaticRestartsBeforeFirstACKAreReported(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := &countedManager{fakeManager: fakeManager{kind: "systemd"}, automatic: 2}
	f.u.config.Manager = manager
	_, e := f.u.Apply(ctx, f.assignment)
	must(t, e)
	f.u.config.Installed = imageOf(f.next, "v2.0.0")
	ev := HealthEvidence{ProcessID: "first-visible-process", Version: "v2.0.0", ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now}
	_, e = f.u.Observe(ctx, ev)
	must(t, e)
	if f.read(t).Restarts != 3 {
		t.Fatal("pre-ACK automatic restarts omitted")
	}
	manager.automatic = 3
	ev.ProcessID = "next-process"
	f.now = f.now.Add(time.Second)
	ev.At = f.now
	_, e = f.u.Observe(ctx, ev)
	must(t, e)
	if f.read(t).Restarts != 4 {
		t.Fatal("PID and automatic counter double counted")
	}
	manager.err = errInjected
	_, e = f.u.Observe(ctx, ev)
	wantError(t, ErrManager, e)
	manager.err = nil
	manager.automatic = -1
	_, e = f.u.Observe(ctx, ev)
	wantError(t, ErrManager, e)
}
