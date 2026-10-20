//go:build linux

package agentupdate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func completeFixtureHealth(t *testing.T, f *fixture) {
	t.Helper()
	f.u.config.Installed = imageOf(f.next, "v2.0.0")
	for n := 0; n < 3; n++ {
		f.now = f.now.Add(time.Second)
		_, err := f.u.Observe(t.Context(), HealthEvidence{ProcessID: "fixture-process", Version: "v2.0.0", ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: f.now})
		must(t, err)
	}
	_, err := ResolveInstalled(t.Context(), f.u.config)
	must(t, err)
}

func TestHelperReplacementRequiresPreviousSignedProof(t *testing.T) {
	f := newFixture(t)
	bootstrapFixture(t, f)
	must(t, InstallHelper(t.Context(), f.u.config))
	must(t, CheckHelper(t.Context(), f.u.config))
	_, e := f.u.Apply(t.Context(), f.assignment)
	must(t, e)
	completeFixtureHealth(t, f)
	must(t, InstallHelper(t.Context(), f.u.config))
	must(t, CheckHelper(t.Context(), f.u.config))
	data, e := os.ReadFile(filepath.Join(filepath.Dir(f.u.config.Executable), HelperFilename))
	must(t, e)
	if !bytes.Equal(f.next, data) {
		t.Fatal("helper did not advance to verified current image")
	}
}

func TestRollbackReceiptRejectsDamagedPreviousProof(t *testing.T) {
	for _, mode := range []string{"missing", "signature"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			bootstrapFixture(t, f)
			_, e := f.u.Apply(t.Context(), f.assignment)
			must(t, e)
			completeFixtureHealth(t, f)
			path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, installedReceiptFile)
			data, e := os.ReadFile(path)
			must(t, e)
			var receipt installedReceipt
			must(t, json.Unmarshal(data, &receipt))
			if mode == "missing" {
				receipt.Previous = nil
			} else {
				receipt.Previous.Manifest.Signature[0] ^= 1
			}
			data, e = json.Marshal(receipt)
			must(t, e)
			must(t, os.WriteFile(path, data, 0o600))
			_, e = f.u.Rollback(t.Context())
			must(t, e)
			_, e = ResolveInstalled(t.Context(), f.u.config)
			wantError(t, ErrJournal, e)
		})
	}
}

func TestRuntimeMetadataRejectsCorruptionAndOversize(t *testing.T) {
	f := newFixture(t)
	wantError(t, ErrManualOnly, f.u.Enqueue(f.assignment))
	wantError(t, ErrManualOnly, VerifyHelper(t.Context(), f.u.config))
	wantError(t, ErrConfiguration, StoreTrust(f.u.config.Executable, nil))
	bootstrapFixture(t, f)
	inst := f.open(t)
	wantError(t, ErrJournal, saveInstalled(inst, installedReceipt{Current: InstallationProof{Manifest: f.assignment.Manifest, ArtifactID: string(bytes.Repeat([]byte{'a'}, maximumJournalBytes))}}))
	must(t, writePrivate(inst.state, trustFile, []byte("{")))
	inst.close()
	_, e := LoadTrust(f.u.config.Executable)
	wantError(t, ErrJournal, e)
	path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory)
	must(t, os.WriteFile(filepath.Join(path, helperProofFile), []byte("{"), 0o600))
	wantError(t, ErrJournal, VerifyHelper(t.Context(), f.u.config))
	must(t, os.WriteFile(filepath.Join(path, installedReceiptFile), []byte("{"), 0o600))
	if e = InstallHelper(t.Context(), f.u.config); e == nil {
		t.Fatal("malformed receipt accepted")
	}
	f.next = f.old
	f.resign(t, "v1.0.0")
	_, e = Bootstrap(t.Context(), BootstrapOptions{Config: f.u.config, Manifest: f.assignment.Manifest, ArtifactID: "linux"})
	wantError(t, ErrJournal, e)
}

func TestBootstrapNeverReanchorsAnExistingInstallation(t *testing.T) {
	f := newFixture(t)
	bootstrapFixture(t, f)
	must(t, os.WriteFile(f.u.config.Executable, f.next, 0o700))
	_, e := Bootstrap(t.Context(), BootstrapOptions{Config: f.u.config, Manifest: f.assignment.Manifest, ArtifactID: "linux"})
	wantError(t, ErrConflict, e)
	f.checkCurrent(t, f.next)
}

func TestDefaultProofClockAndMalformedState(t *testing.T) {
	f := newFixture(t)
	f.now = time.Now().UTC()
	f.u.config.Keys[0].NotBefore = f.now.Add(-time.Hour)
	f.u.config.Keys[0].ExpiresAt = f.now.Add(2 * time.Hour)
	f.next = f.old
	f.resign(t, "v1.0.0")
	config := f.u.config
	config.Now = nil
	_, e := Bootstrap(t.Context(), BootstrapOptions{Config: config, Manifest: f.assignment.Manifest, ArtifactID: "linux"})
	must(t, e)
	_, e = ResolveInstalled(t.Context(), config)
	must(t, e)
	config.Executable = filepath.Join(t.TempDir(), "missing", "beat-agent")
	if _, e = Bootstrap(t.Context(), BootstrapOptions{Config: config, Manifest: f.assignment.Manifest, ArtifactID: "linux"}); e == nil {
		t.Fatal("missing root accepted")
	}
	if e = decodePrivateJSON([]byte("{} {}"), new(any)); e == nil {
		t.Fatal("multiple metadata objects accepted")
	}
}

func TestMailboxDoesNotIgnoreCorruptJournal(t *testing.T) {
	f := newFixture(t)
	bootstrapFixture(t, f)
	must(t, f.u.Enqueue(f.assignment))
	path := filepath.Join(filepath.Dir(f.u.config.Executable), stateDirectory, journalFile)
	must(t, os.WriteFile(path, []byte("{"), 0o600))
	wantError(t, ErrJournal, f.u.Enqueue(f.assignment))
	_, e := f.u.Pending()
	wantError(t, ErrJournal, e)
	_, e = f.u.Observation(f.now)
	wantError(t, ErrJournal, e)
}
