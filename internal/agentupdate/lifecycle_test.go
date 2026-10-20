//go:build linux

package agentupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyHealthAndRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var events []Event
	f.u.SetObserver(func(e Event) { events = append(events, e) })
	s, e := f.u.Recover(ctx)
	wantPhase(t, "idle", s, e)
	s, e = f.u.Apply(ctx, f.assignment)
	wantPhase(t, "published", s, e)
	f.checkCurrent(t, f.next)
	if f.manager.calls != 1 || len(events) < 2 {
		t.Fatal("restart/events missing")
	}
	oldProcess := f.u
	evidence := HealthEvidence{ProcessID: "fixture-process", Version: "v2.0.0", ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now}
	_, e = oldProcess.Observe(ctx, evidence)
	wantError(t, ErrHealth, e)
	config := f.u.config
	config.Installed = imageOf(f.next, "v2.0.0")
	f.u, e = New(config)
	must(t, e)
	for n := 0; n < 3; n++ {
		f.now = f.now.Add(time.Second)
		evidence.At = f.now
		s, e = f.u.Observe(ctx, evidence)
		must(t, e)
	}
	if s.Phase != "healthy" {
		t.Fatal(s)
	}
	s, e = f.u.Recover(ctx)
	wantPhase(t, "healthy", s, e)
	s, e = f.u.Rollback(ctx)
	wantPhase(t, "rolled_back", s, e)
	f.checkCurrent(t, f.old)
	s, e = f.u.Rollback(ctx)
	wantPhase(t, "rolled_back", s, e)
	s, e = f.u.Recover(ctx)
	wantPhase(t, "rolled_back", s, e)
	if f.manager.calls != 2 {
		t.Fatal(f.manager.calls)
	}
}

func TestCrashRecoveryKeepsEntrypoint(t *testing.T) {
	for _, point := range []string{"download_chunk", "previous_synced", "prepared", "publish_intent", "published_rename", "published", "restart_requested"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			f.crashAt(point)
			_, e := f.u.Apply(ctx, f.assignment)
			wantError(t, errInjected, e)
			_, e = os.Stat(f.u.config.Executable)
			must(t, e)
			f.u.fault = nil
			s, e := f.u.Recover(ctx)
			must(t, e)
			if s.Phase == "downloading" || s.Phase == "prepared" {
				s, e = f.u.Apply(ctx, f.assignment)
			}
			wantPhase(t, "published", s, e)
			f.checkCurrent(t, f.next)
			f.now = f.now.Add(2 * time.Minute)
			s, e = f.u.Recover(ctx)
			wantPhase(t, "rolled_back", s, e)
			f.checkCurrent(t, f.old)
		})
	}
}

func TestRollbackCrashRecovery(t *testing.T) {
	for _, point := range []string{"rollback_prepared", "rollback_rename"} {
		t.Run(point, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			_, e := f.u.Apply(ctx, f.assignment)
			must(t, e)
			f.crashAt(point)
			_, e = f.u.Rollback(ctx)
			wantError(t, errInjected, e)
			_, e = os.Stat(f.u.config.Executable)
			must(t, e)
			f.u.fault = nil
			s, e := f.u.Recover(ctx)
			wantPhase(t, "rolled_back", s, e)
			f.checkCurrent(t, f.old)
		})
	}
}

func TestCancelAndManagerFailure(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		f.crashAt("prepared")
		_, e := f.u.Apply(ctx, f.assignment)
		wantError(t, errInjected, e)
		s, e := f.u.Cancel(ctx)
		wantPhase(t, "canceled", s, e)
		f.checkCurrent(t, f.old)
		_, e = f.u.Cancel(ctx)
		wantError(t, ErrBusy, e)
		f.u.fault = nil
		s, e = f.u.Apply(ctx, f.assignment)
		wantPhase(t, "published", s, e)
		_, e = f.u.Cancel(ctx)
		wantError(t, ErrBusy, e)
	})
	t.Run("restart-failed", func(t *testing.T) {
		f := newFixture(t)
		f.manager.failures = 1
		s, e := f.u.Apply(context.Background(), f.assignment)
		wantError(t, ErrManager, e)
		if s.Phase != "rolled_back" || f.manager.calls != 2 {
			t.Fatal(s, f.manager.calls)
		}
		f.checkCurrent(t, f.old)
	})
}

func TestRecoveryRejectsExternalChanges(t *testing.T) {
	for _, phase := range []string{"download_chunk", "prepared", "published"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			f.crashAt(phase)
			_, e := f.u.Apply(context.Background(), f.assignment)
			wantError(t, errInjected, e)
			must(t, os.WriteFile(f.u.config.Executable, []byte("external change"), 0o600))
			_, e = f.u.Recover(context.Background())
			wantError(t, ErrConflict, e)
			_, e = f.u.Rollback(context.Background())
			wantError(t, ErrConflict, e)
			_, e = f.u.Cancel(context.Background())
			if e == nil {
				t.Fatal("cancel accepted conflict")
			}
		})
	}
	t.Run("previous-corrupt", func(t *testing.T) {
		f := newFixture(t)
		_, e := f.u.Apply(context.Background(), f.assignment)
		must(t, e)
		must(t, os.WriteFile(filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, previousFile), []byte("corrupt"), 0o600))
		_, e = f.u.Rollback(context.Background())
		wantError(t, ErrIntegrity, e)
		f.checkCurrent(t, f.next)
	})
}

func TestHealthWindowAndFailure(t *testing.T) {
	for _, kind := range []string{"invalid", "deadline", "gap", "backdated", "future", "wrong-version"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			_, e := f.u.Apply(ctx, f.assignment)
			must(t, e)
			f.u.config.Installed = imageOf(f.next, "v2.0.0")
			ev := HealthEvidence{ProcessID: "fixture-process", Version: "v2.0.0", ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now}
			switch kind {
			case "invalid":
				ev.ValidSample = false
			case "deadline":
				f.now = f.now.Add(3 * time.Minute)
				ev.At = f.now
			case "backdated":
				ev.At = f.now.Add(-time.Second)
			case "future":
				ev.At = f.now.Add(time.Minute)
			case "wrong-version":
				ev.Version = "v1.0.0"
			case "gap":
				_, e = f.u.Observe(ctx, ev)
				must(t, e)
				f.now = f.now.Add(3 * time.Second)
				ev.At = f.now
			}
			s, e := f.u.Observe(ctx, ev)
			if kind == "future" || kind == "backdated" {
				wantError(t, ErrHealth, e)
				return
			}
			must(t, e)
			if kind == "gap" {
				if s.Phase != "observing" {
					t.Fatal(s)
				}
				return
			}
			if s.Phase != "rolled_back" {
				t.Fatal(s)
			}
			f.checkCurrent(t, f.old)
		})
	}
}
