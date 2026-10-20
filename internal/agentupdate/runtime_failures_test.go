//go:build linux

package agentupdate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestAllRuntimeAPIsRejectSymlinkedState(t *testing.T) {
	f := newFixture(t)
	must(t, os.Symlink(t.TempDir(), filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory)))
	ctx := context.Background()
	checks := []func() error{
		func() error {
			_, e := Bootstrap(ctx, BootstrapOptions{Config: f.u.config, Manifest: f.assignment.Manifest, ArtifactID: "linux"})
			return e
		},
		func() error { _, e := ResolveInstalled(ctx, f.u.config); return e },
		func() error { return StoreTrust(f.u.config.Executable, f.u.config.Keys) },
		func() error { _, e := LoadTrust(f.u.config.Executable); return e },
		func() error { return InstallHelper(ctx, f.u.config) },
		func() error { return VerifyHelper(ctx, f.u.config) },
		func() error { return f.u.Enqueue(f.assignment) },
		func() error { _, e := f.u.Pending(); return e },
		func() error { _, e := f.u.Observation(f.now); return e },
		func() error { _, e := f.u.Capabilities(); return e },
	}
	for _, check := range checks {
		if e := check(); e == nil {
			t.Fatal("symlinked state accepted")
		}
	}
}

func TestControllerKeepsNonceAcrossLocalLockContention(t *testing.T) {
	f := newFixture(t)
	bootstrapFixture(t, f)
	observations := 0
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		observations++
		if r.URL.Path != observationPath {
			t.Error("unexpected lease retry")
		}
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200}); e != nil {
			t.Error(e)
		}
	})
	controller := Controller{updater: f.u, Client: client, pending: &f.assignment}
	inst := f.open(t)
	wantError(t, ErrBusy, controller.poll(t.Context()))
	if controller.pending == nil {
		t.Fatal("lost only copy of nonce")
	}
	inst.close()
	must(t, controller.poll(t.Context()))
	if controller.pending != nil || observations != 0 {
		t.Fatal("pending not committed")
	}
	controller.pending = &f.assignment
	f.assignment.ServerSchema = 1
	wantError(t, ErrAssignment, controller.poll(t.Context()))
	if controller.pending != nil || observations != 1 {
		t.Fatal("rejection was not reported")
	}
}

func TestControllerAndSupervisorErrorLoopsRemainCancelable(t *testing.T) {
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil}); e != nil {
			t.Error(e)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	controller := Controller{Client: client, Build: func(context.Context) (*Updater, error) { return nil, ErrManualOnly }, OnError: func(error) { cancel() }}
	must(t, controller.Run(ctx))
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	supervisor := Supervisor{Build: func(context.Context) (*Updater, error) { return nil, ErrManualOnly }, OnError: func(error) { stop() }}
	must(t, supervisor.Run(ctx))
}

func TestControllerVerifiesRealRunningImageBeforePolling(t *testing.T) {
	f := newFixture(t)
	data, e := os.ReadFile("/proc/self/exe")
	must(t, e)
	must(t, os.WriteFile(f.u.config.Executable, data, 0o700))
	f.u.config.Installed = imageOf(data, "v1.0.0")
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil}); e != nil {
			t.Error(e)
		}
	})
	controller := Controller{RunningVersion: "v1.0.0", Client: client, Build: func(context.Context) (*Updater, error) { return f.u, nil }}
	must(t, controller.step(t.Context()))
	if controller.updater == nil || controller.processID == "" {
		t.Fatal("running process was not verified")
	}
	mislabeled := Controller{RunningVersion: "v2.0.0", Client: client, Build: func(context.Context) (*Updater, error) { return f.u, nil }}
	wantError(t, ErrVersion, mislabeled.step(t.Context()))

}

func TestHelperReportsRollbackWithoutInventingHealth(t *testing.T) {
	f := newFixture(t)
	_, e := f.u.Apply(t.Context(), f.assignment)
	must(t, e)
	_, e = f.u.Rollback(t.Context())
	must(t, e)
	calls := 0
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var value map[string]any
		if e := json.NewDecoder(r.Body).Decode(&value); e != nil {
			t.Error(e)
		}
		if value["outcome"] != "rolled_back" || value["version"] != "v1.0.0" {
			t.Error(value)
		}
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200}); e != nil {
			t.Error(e)
		}
	})
	f.u.config.Fetcher = client.fetcher
	supervisor := Supervisor{}
	must(t, supervisor.reportRollback(t.Context(), f.u))
	must(t, supervisor.reportRollback(t.Context(), f.u))
	if calls != 1 {
		t.Fatal("terminal observation repeated")
	}
}

func TestCumulativeDownloadBudgetDoesNotResetOnResume(t *testing.T) {
	f := newFixture(t)
	f.crashAt("download_chunk")
	_, e := f.u.Apply(t.Context(), f.assignment)
	wantError(t, errInjected, e)
	f.u.fault = nil
	f.now = f.now.Add(11 * time.Minute)
	_, e = f.u.Apply(t.Context(), f.assignment)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("download budget extended", e)
	}
	f.checkCurrent(t, f.old)
}

func TestControlClientRejectsMalformedAndUnboundedBodies(t *testing.T) {
	_, e := NewControlClient(HTTPConfig{ServerURL: "http://beat.test", AgentToken: "x"})
	wantError(t, ErrConfiguration, e)
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, e := w.Write([]byte(`{"code":200,"data":{"unknown":true}}`))
		if e != nil {
			t.Error(e)
		}
	})
	_, e = client.Lease(t.Context(), protocol.Capabilities{})
	wantError(t, ErrDownload, e)
	_, e = client.post(t.Context(), leasePath, func() {})
	wantError(t, ErrAssignment, e)
	client.fetcher.server.Host = "bad\n"
	_, e = client.post(t.Context(), leasePath, nil)
	wantError(t, ErrDownload, e)
}

func TestRuntimeAdmissionAndErrorCodes(t *testing.T) {
	f := newFixture(t)
	f.u.config.Container = true
	wantError(t, ErrExternalManager, f.u.Enqueue(f.assignment))
	_, e := Bootstrap(t.Context(), BootstrapOptions{Config: f.u.config})
	wantError(t, ErrExternalManager, e)
	f.u.config.Container = false
	f.u.config.OS = "windows"
	_, e = Bootstrap(t.Context(), BootstrapOptions{Config: f.u.config})
	wantError(t, ErrManualOnly, e)
	for err, want := range map[error]string{ErrDisabled: "disabled", ErrExternalManager: "external_manager", ErrManualOnly: "manual_only", ErrBusy: "busy", protocol.ErrSignature: "trust_rejected", ErrConflict: "conflict", errInjected: "update_failed"} {
		if got := ErrorCode(err); got != want {
			t.Fatal(want, got)
		}
	}
}

type readyManager struct {
	fakeManager
	readyErr error
}

func (manager *readyManager) Ready(context.Context) error { return manager.readyErr }

func TestControllerDoesNotLeaseWithoutReadyWatchdog(t *testing.T) {
	f := newFixture(t)
	calls := 0
	manager := &readyManager{fakeManager: fakeManager{kind: "systemd"}, readyErr: ErrManualOnly}
	f.u.config.Manager = manager
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil}); e != nil {
			t.Error(e)
		}
	})
	controller := Controller{updater: f.u, Client: client}
	wantError(t, ErrManualOnly, controller.poll(t.Context()))
	if calls != 0 {
		t.Fatal("offered update without watchdog")
	}
	manager.readyErr = nil
	controller.nextPoll = time.Time{}
	must(t, controller.poll(t.Context()))
	if calls != 1 {
		t.Fatal("ready watchdog did not permit poll")
	}
}

func TestExpiredDownloadReleasesLocalAssignmentForExplicitRetry(t *testing.T) {
	f := newFixture(t)
	bootstrapFixture(t, f)
	must(t, f.u.Enqueue(f.assignment))
	f.crashAt("download_chunk")
	supervisor := Supervisor{Build: func(context.Context) (*Updater, error) { return f.u, nil }}
	wantError(t, errInjected, supervisor.step(t.Context()))
	f.u.fault = nil
	f.now = f.now.Add(11 * time.Minute)
	wantError(t, context.DeadlineExceeded, supervisor.step(t.Context()))
	if f.read(t).Phase != "canceled" {
		t.Fatal("expired download still blocks future owner retry")
	}
	pending, e := f.u.Pending()
	must(t, e)
	if pending != nil {
		t.Fatal("expired mailbox retained")
	}
	f.checkCurrent(t, f.old)
	f.assignment.ID = "explicit-owner-retry"
	f.assignment.ExpiresAt = f.now.Add(20 * time.Minute)
	must(t, f.u.Enqueue(f.assignment))
	must(t, supervisor.step(t.Context()))
	f.checkCurrent(t, f.next)
}
