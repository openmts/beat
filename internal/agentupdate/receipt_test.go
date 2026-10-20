//go:build linux

package agentupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func bootstrapFixture(t *testing.T, f *fixture) {
	t.Helper()
	next := f.next
	f.next = f.old
	f.resign(t, "v1.0.0")
	image, err := Bootstrap(context.Background(), BootstrapOptions{Config: f.u.config, Manifest: f.assignment.Manifest, ArtifactID: f.assignment.Artifact.ID})
	must(t, err)
	if !sameImage(image, f.u.config.Installed) {
		t.Fatal("bootstrap changed anchor")
	}
	f.next = next
	f.resign(t, "v2.0.0")
	must(t, StoreTrust(f.u.config.Executable, f.u.config.Keys))
}

func TestSignedReceiptLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bootstrapFixture(t, f)
	image, e := ResolveInstalled(ctx, f.u.config)
	must(t, e)
	if !sameImage(image, f.u.config.Installed) {
		t.Fatal("initial image changed")
	}
	keys, e := LoadTrust(f.u.config.Executable)
	must(t, e)
	if len(keys) != 1 {
		t.Fatal(keys)
	}
	must(t, InstallHelper(ctx, f.u.config))
	data, e := os.ReadFile(filepath.Join(filepath.Dir(f.u.config.Executable), HelperFilename))
	must(t, e)
	if string(data) != string(f.old) {
		t.Fatal("helper differs from verified source")
	}
	must(t, InstallHelper(ctx, f.u.config))
	_, e = f.u.Apply(ctx, f.assignment)
	must(t, e)
	image, e = ResolveInstalled(ctx, f.u.config)
	must(t, e)
	if !sameImage(image, imageOf(f.next, "v2.0.0")) {
		t.Fatal("target not resolved")
	}
	f.u.config.Installed = image
	for n := 0; n < 3; n++ {
		f.now = f.now.Add(time.Second)
		_, e = f.u.Observe(ctx, HealthEvidence{ProcessID: "process-one", Version: image.Version, ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now})
		must(t, e)
	}
	_, e = ResolveInstalled(ctx, f.u.config)
	must(t, e)
	inst := f.open(t)
	raw, e := readPrivate(inst.state, installedReceiptFile, maximumJournalBytes)
	must(t, e)
	inst.close()
	var receipt installedReceipt
	must(t, json.Unmarshal(raw, &receipt))
	if receipt.Previous == nil {
		t.Fatal("previous proof discarded")
	}
	_, e = f.u.Rollback(ctx)
	must(t, e)
	image, e = ResolveInstalled(ctx, f.u.config)
	must(t, e)
	if !sameImage(image, imageOf(f.old, "v1.0.0")) {
		t.Fatal("rollback receipt not restored")
	}
}

func TestReceiptRejectsUnknownOrDamagedInstallations(t *testing.T) {
	for _, kind := range []string{"missing", "unknown-file", "signature", "key", "receipt", "future", "helper-unknown", "helper-proof", "wrong-artifact"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			if kind != "missing" {
				bootstrapFixture(t, f)
			}
			state := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory)
			switch kind {
			case "unknown-file":
				must(t, os.WriteFile(f.u.config.Executable, []byte("unknown"), 0o600))
			case "key":
				f.u.config.Keys = nil
			case "receipt":
				must(t, os.WriteFile(filepath.Join(state, installedReceiptFile), []byte("{}"), 0o600))
			case "signature", "future", "wrong-artifact":
				data, e := os.ReadFile(filepath.Join(state, installedReceiptFile))
				must(t, e)
				var r installedReceipt
				must(t, json.Unmarshal(data, &r))
				if kind == "signature" {
					r.Current.Manifest.Signature[0] ^= 1
				}
				if kind == "future" {
					r.Current.VerifiedAt = f.now.Add(time.Hour)
				}
				if kind == "wrong-artifact" {
					r.Current.ArtifactID = "missing"
				}
				data, e = json.Marshal(r)
				must(t, e)
				must(t, os.WriteFile(filepath.Join(state, installedReceiptFile), data, 0o600))
			case "helper-unknown":
				must(t, os.WriteFile(filepath.Join(filepath.Dir(f.u.config.Executable), HelperFilename), []byte("foreign"), 0o700))
				if e := InstallHelper(ctx, f.u.config); e == nil {
					t.Fatal("overwrote unknown helper")
				}
				return
			case "helper-proof":
				must(t, InstallHelper(ctx, f.u.config))
				if e := VerifyHelper(ctx, f.u.config); e == nil {
					t.Fatal("test process impersonated signed helper")
				}
				return
			}
			if _, e := ResolveInstalled(ctx, f.u.config); e == nil {
				t.Fatal("untrusted installation accepted")
			}
		})
	}
}

func TestBootstrapRequiresSignedCurrentBinary(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, e := Bootstrap(ctx, BootstrapOptions{Config: f.u.config, Manifest: f.assignment.Manifest, ArtifactID: f.assignment.Artifact.ID})
	wantError(t, ErrConflict, e)
	bootstrapFixture(t, f)
	next := f.next
	f.next = f.old
	f.resign(t, "v1.0.0")
	_, e = Bootstrap(ctx, BootstrapOptions{Config: f.u.config, Manifest: f.assignment.Manifest, ArtifactID: f.assignment.Artifact.ID})
	must(t, e)
	f.next = next
	f.resign(t, "v2.0.0")
	f.u.config.OS = "windows"
	wantError(t, ErrManualOnly, f.u.VerifyRunningImage(ctx))
}

func TestProcessRestartResetsHealthWindow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, e := f.u.Apply(ctx, f.assignment)
	must(t, e)
	f.u.config.Installed = imageOf(f.next, "v2.0.0")
	ev := HealthEvidence{ProcessID: "process-one", Version: "v2.0.0", ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now}
	_, e = f.u.Observe(ctx, ev)
	must(t, e)
	f.now = f.now.Add(time.Second)
	ev.At = f.now
	ev.ProcessID = "process-two"
	s, e := f.u.Observe(ctx, ev)
	wantPhase(t, "observing", s, e)
	f.now = f.now.Add(time.Second)
	ev.At = f.now
	s, e = f.u.Observe(ctx, ev)
	wantPhase(t, "observing", s, e)
	f.now = f.now.Add(time.Second)
	ev.At = f.now
	s, e = f.u.Observe(ctx, ev)
	wantPhase(t, "healthy", s, e)
	o, e := f.u.Observation(f.now)
	must(t, e)
	if o == nil || o.Restarts != 2 {
		t.Fatal(o)
	}
	ev.ProcessID = "process-three"
	_, e = f.u.Observe(ctx, ev)
	must(t, e)
	o, e = f.u.Observation(f.now)
	must(t, e)
	if o.Restarts != 3 {
		t.Fatal(o)
	}
	identity, e := processIdentity()
	must(t, e)
	if identity == "" {
		t.Fatal("no real identity")
	}
	for _, raw := range []string{"", ") ", "1 (bad) state 1"} {
		if _, e = parseProcessIdentity(raw, 1); e == nil {
			t.Fatal("malformed process identity")
		}
	}
}
