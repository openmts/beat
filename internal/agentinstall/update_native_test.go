//go:build linux

package agentinstall

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/agentupdate"
)

func TestNativeIndependentWatchdogRollback(t *testing.T) {
	if os.Getenv("BEAT_RUN_UPDATE_SYSTEMD_FIXTURE") != "1" {
		t.Skip("explicit isolated systemd fixture only")
	}
	if os.Geteuid() != 0 {
		t.Fatal("native fixture requires root")
	}
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "etc"))
	mustMkdir(t, filepath.Join(root, "opt"))
	mustMkdir(t, filepath.Join(root, "started"))
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	control := nativeUpdateControl{Root: root, Unit: "/run/systemd/system/beat-update-fixture-" + suffix + ".service", HelperUnit: "/run/systemd/system/beat-watchdog-fixture-" + suffix + ".service"}
	installer := nativeInstaller(control)
	original, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := os.ReadFile("/usr/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, installer.paths.binary, original, executableFile)
	mustWrite(t, filepath.Join(root, "candidate.bin"), candidate, privateFile)
	writeUpdateJSON(t, filepath.Join(root, "etc", "fixture.json"), control)
	config := agentjoin.Config{ServerURL: "https://fixture.invalid", AgentToken: "private-fixture-token", NodeName: "native-update-fixture", AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: "1s"}
	writeUpdateJSON(t, installer.paths.config, config)
	if err = installer.enableUpdateSettings(installer.paths.config, agentupdate.Settings{Enabled: true, NodeID: "native-node"}); err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{73}, 32))
	now := time.Now().UTC()
	keys := []protocol.SigningTrust{{KeyID: "fixture", PublicKey: key.Public().(ed25519.PublicKey), NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour)}}
	core := agentupdate.Config{Enabled: true, Executable: installer.paths.binary, NodeID: "native-node", ServerURL: config.ServerURL, Keys: keys, OS: "linux", Arch: runtime.GOARCH, AgentProtocol: 2, Now: func() time.Time { return time.Now().UTC() }}
	core.Installed, err = agentupdate.Bootstrap(t.Context(), agentupdate.BootstrapOptions{Config: core, Manifest: signedUpdate(t, key, original, "v1.0.0", now), ArtifactID: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if err = agentupdate.StoreTrust(core.Executable, keys); err != nil {
		t.Fatal(err)
	}
	if err = agentupdate.InstallHelper(t.Context(), core); err != nil {
		t.Fatal(err)
	}
	createNativeUnit(t, control.Unit, unitFile(installer.paths.binary, installer.paths.config))
	createNativeUnit(t, control.HelperUnit, helperUnitFile(installer.helperBinaryPath(), installer.paths.config))
	maskNativeTimeoutDropIn(t, control.Unit)
	maskNativeTimeoutDropIn(t, control.HelperUnit)
	t.Cleanup(func() { cleanupNativeUnits(t, control) })
	if output, err := nativeSystemctl("daemon-reload"); err != nil {
		t.Fatal(output, err)
	}
	if output, err := nativeSystemctl("start", filepath.Base(control.Unit), filepath.Base(control.HelperUnit)); err != nil {
		t.Fatal(output, err)
	}
	nativeWait(t, func() bool { return nativeStarted(root) >= 1 })
	helperPID, err := nativeSystemctl("show", "--property=MainPID", "--value", filepath.Base(control.HelperUnit))
	if err != nil || helperPID == "0" {
		t.Fatal(helperPID, err)
	}
	var updater *agentupdate.Updater
	nativeWait(t, func() bool {
		updater, err = installer.BuildUpdater(t.Context(), installer.paths.config)
		if err != nil && !errors.Is(err, agentupdate.ErrBusy) {
			output, queryErr := installer.systemctl(t.Context(), "show", ServiceName, "--property=FragmentPath", "--property=DropInPaths", "--property=LoadState", "--no-pager")
			actual, readErr := readPrivate(installer.paths.unit)
			t.Fatalf("fixture admission: %v; host=%v; unit_match=%v read=%v; properties=%q query=%v", err, installer.checkHost(t.Context()), string(actual) == string(unitFile(installer.paths.binary, installer.paths.config)), readErr, output, queryErr)
		}
		return err == nil
	})
	signed := signedUpdate(t, key, candidate, "v2.0.0", now)
	manifest, err := protocol.DecodeManifest(signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	assignment := protocol.Assignment{Action: protocol.AssignmentUpgrade, ServerSchema: 14, ID: "native-assignment", RolloutID: "native-rollout", NodeID: "native-node", Nonce: strings.Repeat("a", 64), ExpiresAt: now.Add(2 * time.Minute), Artifact: manifest.Artifacts[0], Manifest: signed}
	nativeWait(t, func() bool { return updater.Enqueue(assignment) == nil })
	nativeWait(t, func() bool { return nativeJournalPhase(root) == "rolled_back" && nativeStarted(root) >= 2 })
	restored, err := os.ReadFile(installer.paths.binary)
	if err != nil || !bytes.Equal(original, restored) {
		t.Fatal("verified old entrypoint not restored", err)
	}
	afterPID, err := nativeSystemctl("show", "--property=MainPID", "--value", filepath.Base(control.HelperUnit))
	if err != nil || helperPID != afterPID {
		t.Fatal("independent watchdog restarted or died", helperPID, afterPID, err)
	}
	t.Log("fixture-only masks disabled host global 10-timeout-abort.conf; test adapter ignored only the verified /dev/null mask item; production admission was unchanged")
	t.Logf("isolated systemd rollback verified: helper PID %s remained alive; old Agent restored; starts=%d", helperPID, nativeStarted(root))
}

func createNativeUnit(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func nativeStarted(root string) int {
	entries, err := os.ReadDir(filepath.Join(root, "started"))
	if err != nil {
		return 0
	}
	return len(entries)
}

func nativeJournalPhase(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "opt", ".beat-agent-update", "journal.json"))
	if err != nil {
		return ""
	}
	var state struct {
		Phase string `json:"phase"`
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return ""
	}
	return state.Phase
}

func cleanupNativeUnits(t *testing.T, control nativeUpdateControl) {
	t.Helper()
	names := []string{filepath.Base(control.Unit), filepath.Base(control.HelperUnit)}
	if output, err := nativeSystemctl(append([]string{"stop"}, names...)...); err != nil {
		t.Error("stop own fixture units", output, err)
	}
	if output, err := nativeSystemctl(append([]string{"reset-failed"}, names...)...); err != nil && !strings.Contains(output, "not loaded") {
		t.Error("reset own fixture units", output, err)
	}
	for _, path := range []string{control.Unit, control.HelperUnit} {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		removeNativeMask(t, path)
	}
	if output, err := nativeSystemctl("daemon-reload"); err != nil {
		t.Error(output, err)
	}
}

func maskNativeTimeoutDropIn(t *testing.T, unit string) {
	t.Helper()
	directory := unit + ".d"
	if err := os.Mkdir(directory, privateDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeNativeMask(t, unit) })
	if err := os.Symlink("/dev/null", filepath.Join(directory, "10-timeout-abort.conf")); err != nil {
		t.Fatal(err)
	}
}

func removeNativeMask(t *testing.T, unit string) {
	t.Helper()
	for _, path := range []string{filepath.Join(unit+".d", "10-timeout-abort.conf"), unit + ".d"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	}
}
