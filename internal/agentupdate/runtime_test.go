//go:build linux

package agentupdate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func controlFixture(t *testing.T, handler http.HandlerFunc) *ControlClient {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, e := NewControlClient(HTTPConfig{ServerURL: server.URL, AgentToken: "private-token", TLS: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}})
	must(t, e)
	t.Cleanup(client.Close)
	return client
}

func TestControlClientContract(t *testing.T) {
	f := newFixture(t)
	for _, mode := range []string{"assignment", "empty", "missing-code", "bad-code", "status", "redirect", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != leasePath || r.Header.Get("Authorization") != "Bearer private-token" {
					t.Error("incorrect request binding")
				}
				if mode == "status" {
					w.WriteHeader(403)
					return
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://elsewhere.test", http.StatusFound)
					return
				}
				var value any = map[string]any{"code": 200, "message": "success", "data": f.assignment}
				if mode == "empty" {
					value = map[string]any{"code": 200, "data": nil}
				}
				if mode == "missing-code" {
					value = map[string]any{"data": nil}
				}
				if mode == "bad-code" {
					value = map[string]any{"code": 500}
				}
				if mode == "malformed" {
					_, e := w.Write([]byte("{"))
					if e != nil {
						t.Error(e)
					}
					return
				}
				if e := json.NewEncoder(w).Encode(value); e != nil {
					t.Error(e)
				}
			})
			assignment, e := client.Lease(context.Background(), protocol.Capabilities{})
			switch mode {
			case "assignment":
				must(t, e)
				if !sameAssignment(f.assignment, *assignment) {
					t.Fatal("assignment altered")
				}
			case "empty":
				must(t, e)
				if assignment != nil {
					t.Fatal("empty offer altered")
				}
			default:
				wantError(t, ErrDownload, e)
			}
		})
	}
}

func TestMailboxSupervisorResumesAndRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bootstrapFixture(t, f)
	must(t, f.u.Enqueue(f.assignment))
	pending, e := f.u.Pending()
	must(t, e)
	if pending == nil {
		t.Fatal("assignment not persisted")
	}
	f.crashAt("prepared")
	supervisor := Supervisor{Build: func(context.Context) (*Updater, error) { return f.u, nil }}
	wantError(t, errInjected, supervisor.step(ctx))
	f.u.fault = nil
	must(t, supervisor.step(ctx))
	f.checkCurrent(t, f.next)
	pending, e = f.u.Pending()
	must(t, e)
	if pending != nil {
		t.Fatal("published assignment replayed")
	}
	wantError(t, ErrBusy, f.u.Enqueue(f.assignment))
	f.now = f.now.Add(2 * time.Minute)
	must(t, supervisor.step(ctx))
	f.checkCurrent(t, f.old)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	must(t, supervisor.Run(canceled))
	invalid := Supervisor{}
	wantError(t, ErrConfiguration, invalid.Run(ctx))
	supervisor.Build = func(context.Context) (*Updater, error) { return nil, ErrManualOnly }
	wantError(t, ErrManualOnly, supervisor.step(ctx))
}

func TestControllerSendsOnlyFreshAcceptedHealth(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, e := f.u.Apply(ctx, f.assignment)
	must(t, e)
	f.u.config.Installed = imageOf(f.next, "v2.0.0")
	var observations []protocol.Observation
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != observationPath {
			t.Error("unexpected request")
		}
		var observation protocol.Observation
		if e := json.NewDecoder(r.Body).Decode(&observation); e != nil {
			t.Error(e)
		}
		observations = append(observations, observation)
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200}); e != nil {
			t.Error(e)
		}
	})
	controller := Controller{RunningVersion: "v2.0.0", updater: f.u, processID: "fixture-process", Client: client}
	controller.ReportFailed(true)
	controller.ReportFailed(false)
	controller.TaskFailed("task-one")
	controller.TaskFailed("task-one")
	for n := 0; n < 3; n++ {
		f.now = f.now.Add(time.Second)
		controller.report = &acceptedReport{serverAt: f.now, receivedAt: f.now}
		must(t, controller.observe(ctx))
	}
	if len(observations) != 1 || observations[0].Outcome != "healthy" || observations[0].ProtocolErrors != 1 || observations[0].MetricErrors != 1 || observations[0].TaskFailures != 1 {
		t.Fatal(observations)
	}
	must(t, controller.observe(ctx))
	if len(observations) != 1 {
		t.Fatal("timer invented a healthy report")
	}
	controller.report = &acceptedReport{serverAt: f.now, receivedAt: f.now.Add(2 * time.Second)}
	wantError(t, ErrHealth, controller.observe(ctx))
	if len(observations) != 1 {
		t.Fatal("rejected health became a healthy observation")
	}
	controller.ReportAccepted(f.now)
	if controller.report == nil {
		t.Fatal("ACK not queued")
	}
}

func TestControllerLeaseAndAdmission(t *testing.T) {
	f := newFixture(t)
	bootstrapFixture(t, f)
	calls := 0
	client := controlFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var caps protocol.Capabilities
		if e := json.NewDecoder(r.Body).Decode(&caps); e != nil {
			t.Error(e)
		}
		if caps.CurrentVersion != "v1.0.0" || caps.Manager != "systemd" || !caps.UpdateEnabled || caps.AvailableBytes <= 0 {
			t.Error(caps)
		}
		if e := json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": f.assignment}); e != nil {
			t.Error(e)
		}
	})
	controller := Controller{updater: f.u, Client: client, PollInterval: time.Hour}
	must(t, controller.step(context.Background()))
	must(t, controller.step(context.Background()))
	if calls != 1 {
		t.Fatal("unbounded polling")
	}
	pending, e := f.u.Pending()
	must(t, e)
	if pending == nil {
		t.Fatal("lease not queued")
	}
	controller.updater = nil
	controller.Build = func(context.Context) (*Updater, error) { return nil, ErrManualOnly }
	wantError(t, ErrManualOnly, controller.step(context.Background()))
	controller.Build = func(context.Context) (*Updater, error) { return f.u, nil }
	if e = controller.step(context.Background()); e == nil {
		t.Fatal("test process impersonated installed binary")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	must(t, controller.Run(canceled))
	invalid := Controller{}
	wantError(t, ErrConfiguration, invalid.Run(context.Background()))
}

func TestRuntimePrivateStateAndDisabledPaths(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, e := LoadTrust(f.u.config.Executable)
	wantError(t, ErrManualOnly, e)
	pending, e := f.u.Pending()
	must(t, e)
	if pending != nil {
		t.Fatal("unexpected pending")
	}
	observation, e := f.u.Observation(f.now)
	must(t, e)
	if observation != nil {
		t.Fatal("unexpected observation")
	}
	f.u.config.Enabled = false
	wantError(t, ErrDisabled, f.u.Enqueue(f.assignment))
	f.u.config.Enabled = true
	must(t, ValidateSettings(Settings{}, "http://untrusted"))
	wantError(t, ErrConfiguration, ValidateSettings(Settings{Enabled: true}, "https://beat.test"))
	must(t, ValidateSettings(Settings{Enabled: true, NodeID: "node"}, "https://beat.test"))
	bootstrapFixture(t, f)
	path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, pendingAssignmentFile)
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	_, e = f.u.Pending()
	wantError(t, ErrJournal, e)
	f.u.config.Container = true
	_, e = f.u.Capabilities()
	wantError(t, ErrExternalManager, e)
	f.u.config.Container = false
	f.u.config.Enabled = false
	supervisor := Supervisor{Build: func(context.Context) (*Updater, error) { return f.u, nil }}
	must(t, supervisor.step(ctx))
}
