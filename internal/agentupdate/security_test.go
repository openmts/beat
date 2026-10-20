//go:build linux

package agentupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestAssignmentTrustAndDirection(t *testing.T) {
	cases := map[string]func(*fixture){
		"empty-action":      func(f *fixture) { f.assignment.Action = "" },
		"node":              func(f *fixture) { f.assignment.NodeID = "other" },
		"id":                func(f *fixture) { f.assignment.ID = "../escape" },
		"nonce":             func(f *fixture) { f.assignment.Nonce = "secret" },
		"expired":           func(f *fixture) { f.assignment.ExpiresAt = f.now },
		"long-lease":        func(f *fixture) { f.assignment.ExpiresAt = f.now.Add(25 * time.Hour) },
		"payload":           func(f *fixture) { f.assignment.Manifest.Payload = []byte("{}") },
		"signature":         func(f *fixture) { f.assignment.Manifest.Signature[0] ^= 1 },
		"unknown-key":       func(f *fixture) { f.u.config.Keys = nil },
		"duplicate-key":     func(f *fixture) { f.u.config.Keys = append(f.u.config.Keys, f.u.config.Keys[0]) },
		"revoked":           func(f *fixture) { f.u.config.Keys[0].Revoked = true },
		"key-expired":       func(f *fixture) { f.u.config.Keys[0].ExpiresAt = f.now },
		"artifact-mismatch": func(f *fixture) { f.assignment.Artifact.Size++ },
		"platform":          func(f *fixture) { f.assignment.Artifact.Arch = "arm64" },
		"archive":           func(f *fixture) { f.assignment.Artifact.Kind = "archive" },
		"unsupported":       func(f *fixture) { f.assignment.Artifact.Support = "unsupported" },
		"entrypoint":        func(f *fixture) { f.assignment.Artifact.Entrypoint = "other" },
		"schema":            func(f *fixture) { f.assignment.ServerSchema = 1 },
		"same-version":      func(f *fixture) { f.u.config.Installed.Version = "v2.0.0" },
		"downgrade":         func(f *fixture) { f.u.config.Installed.Version = "v3.0.0" },
		"invalid-version":   func(f *fixture) { f.u.config.Installed.Version = "not-version" },
		"rollback-newer":    func(f *fixture) { f.assignment.Action = protocol.AssignmentRollback },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mutate(f)
			if _, err := f.u.verify(f.assignment, f.now); err == nil {
				t.Fatal("untrusted assignment accepted")
			}
			f.checkCurrent(t, f.old)
		})
	}
	f := newFixture(t)
	f.u.config.Installed.Version = "v3.0.0"
	f.assignment.Action = protocol.AssignmentRollback
	_, err := f.u.verify(f.assignment, f.now)
	must(t, err)
	f.assignment.Artifact.ID = "missing"
	_, err = f.u.verify(f.assignment, f.now)
	wantError(t, ErrAssignment, err)
}

func TestConfigAdmission(t *testing.T) {
	f := newFixture(t)
	mutations := map[string]func(*Config){
		"path":            func(c *Config) { c.Executable = "relative" },
		"node":            func(c *Config) { c.NodeID = "" },
		"protocol":        func(c *Config) { c.AgentProtocol = 0 },
		"http":            func(c *Config) { c.ServerURL = "http://beat.test" },
		"server-mismatch": func(c *Config) { c.ServerURL = "https://other.test" },
		"download":        func(c *Config) { c.DownloadTimeout = -time.Second },
		"startup":         func(c *Config) { c.StartupTimeout = time.Hour },
		"observation":     func(c *Config) { c.ObservationWindow = -time.Second },
		"gap":             func(c *Config) { c.MaximumObservationGap = time.Hour },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) { c := f.u.config; mutate(&c); _, err := New(c); wantError(t, ErrConfiguration, err) })
	}
	c := f.u.config
	c.Now = nil
	c.OS = ""
	c.Arch = ""
	c.DownloadTimeout = 0
	c.StartupTimeout = 0
	c.ObservationWindow = 0
	c.MaximumObservationGap = 0
	u, err := New(c)
	must(t, err)
	if u.config.Now().IsZero() {
		t.Fatal("zero clock")
	}
	c.Keys[0].PublicKey[0] ^= 1
	if u.config.Keys[0].PublicKey[0] == c.Keys[0].PublicKey[0] {
		t.Fatal("trust not cloned")
	}
	for _, raw := range []string{"https://user@beat.test", "https://beat.test?q=x", "https://beat.test/#x", "https://beat.test/a%2fb", "https://beat.test/a\\b", ":", "https://", "https://:80"} {
		_, err = trustedServer(raw)
		wantError(t, ErrConfiguration, err)
	}
	for _, mode := range []string{"disabled", "container", "windows", "manager", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			switch mode {
			case "disabled":
				f.u.config.Enabled = false
			case "container":
				f.u.config.Container = true
			case "windows":
				f.u.config.OS = "windows"
			case "manager":
				f.manager.kind = "unknown"
			case "metadata":
				f.u.config.Installed = Image{}
			}
			_, err := f.u.Apply(context.Background(), f.assignment)
			if err == nil {
				t.Fatal("admitted")
			}
			if mode != "disabled" {
				_, err = f.u.Recover(context.Background())
				if err == nil {
					t.Fatal("recover admitted")
				}
				_, err = f.u.Cancel(context.Background())
				if err == nil {
					t.Fatal("cancel admitted")
				}
				_, err = f.u.Observe(context.Background(), HealthEvidence{})
				if err == nil {
					t.Fatal("observe admitted")
				}
				_, err = f.u.Rollback(context.Background())
				if err == nil {
					t.Fatal("rollback admitted")
				}
			}
			_, err = os.Stat(filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory))
			if !os.IsNotExist(err) {
				t.Fatal("gate touched state")
			}
		})
	}
}

func TestJournalBindingAndMalformedState(t *testing.T) {
	mutations := map[string]func(*journal){
		"version":             func(j *journal) { j.Version = 9 },
		"size":                func(j *journal) { j.Downloaded = -1 },
		"phase":               func(j *journal) { j.Phase = "unknown" },
		"path":                func(j *journal) { j.Executable = "/elsewhere" },
		"node":                func(j *journal) { j.Assignment.NodeID = "other" },
		"server":              func(j *journal) { j.ServerURL = "https://other.test" },
		"http":                func(j *journal) { j.ServerURL = "http://beat.test" },
		"manifest":            func(j *journal) { j.Assignment.Manifest.Payload = []byte("{}") },
		"signature":           func(j *journal) { j.Assignment.Manifest.Signature[0] ^= 1 },
		"target":              func(j *journal) { j.TargetVersion = "v2.1.0" },
		"anchor":              func(j *journal) { j.Previous.SHA256 = strings.Repeat("f", 64) },
		"future":              func(j *journal) { j.VerifiedAt = j.VerifiedAt.Add(time.Hour) },
		"action":              func(j *journal) { j.Assignment.Action = "" },
		"deadline-missing":    func(j *journal) { j.Deadline = time.Time{} },
		"publication-early":   func(j *journal) { j.PublishedAt = j.VerifiedAt.Add(-time.Hour) },
		"publication-missing": func(j *journal) { j.PublishedAt = time.Time{} },
		"health":              func(j *journal) { j.Phase = "healthy" },
		"health-time":         func(j *journal) { j.FirstHealthyAt = j.PublishedAt.Add(-time.Second); j.LastHealthyAt = j.PublishedAt },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, e := f.u.Apply(context.Background(), f.assignment)
			must(t, e)
			f.mutate(t, mutate)
			_, e = f.u.Recover(context.Background())
			if e == nil {
				t.Fatal("journal accepted")
			}
		})
	}
	for _, body := range []string{"{", "{} {}", "{\"unknown\":1}", "null"} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			inst := f.open(t)
			must(t, writePrivate(inst.state, journalFile, []byte(body)))
			_, e := readJournal(inst)
			wantError(t, ErrJournal, e)
		})
	}
	t.Run("key-revoked-during-recovery", func(t *testing.T) {
		f := newFixture(t)
		_, e := f.u.Apply(context.Background(), f.assignment)
		must(t, e)
		f.u.config.Keys = nil
		_, e = f.u.Recover(context.Background())
		wantError(t, protocol.ErrSignature, e)
	})
	t.Run("canonical-tamper", func(t *testing.T) {
		f := newFixture(t)
		var m protocol.Manifest
		must(t, json.Unmarshal(f.assignment.Manifest.Payload, &m))
		m.Artifacts[0].Size++
		b, e := protocol.CanonicalManifest(m)
		must(t, e)
		f.assignment.Manifest.Payload = b
		f.assignment.Manifest.Signature = ed25519.Sign(f.key, b)
		_, e = f.u.verify(f.assignment, f.now)
		wantError(t, ErrAssignment, e)
	})
}

func TestLocksAndIdleCalls(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.u.mu.Lock()
	_, e := f.u.Apply(ctx, f.assignment)
	wantError(t, ErrBusy, e)
	_, e = f.u.Recover(ctx)
	wantError(t, ErrBusy, e)
	_, e = f.u.Cancel(ctx)
	wantError(t, ErrBusy, e)
	_, e = f.u.Rollback(ctx)
	wantError(t, ErrBusy, e)
	_, e = f.u.Observe(ctx, HealthEvidence{})
	wantError(t, ErrBusy, e)
	f.u.mu.Unlock()
	for _, call := range []func(context.Context) (Status, error){f.u.Cancel, f.u.Rollback} {
		s, e := call(ctx)
		wantPhase(t, "idle", s, e)
	}
	s, e := f.u.Observe(ctx, HealthEvidence{})
	wantPhase(t, "idle", s, e)
	inst := f.open(t)
	_, e = f.u.Apply(ctx, f.assignment)
	wantError(t, ErrBusy, e)
	_, e = f.u.Recover(ctx)
	wantError(t, ErrBusy, e)
	_, e = f.u.Cancel(ctx)
	wantError(t, ErrBusy, e)
	_, e = f.u.Rollback(ctx)
	wantError(t, ErrBusy, e)
	_, e = f.u.Observe(ctx, HealthEvidence{})
	wantError(t, ErrBusy, e)
	must(t, cleanupJournalTemporary(&installation{}))
	must(t, writePrivate(inst.state, "other", []byte("x")))
}
