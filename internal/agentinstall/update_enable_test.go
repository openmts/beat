//go:build linux

package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentupdate"
)

func TestEnableSignedUpdatesPreservesExistingIdentity(t *testing.T) {
	f := newUpdateFixture(t)
	installer := f.base.installer
	before, err := os.ReadFile(f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = installer.EnableUpdates(t.Context(), f.options); err != nil {
		t.Fatal(err)
	}
	config, err := agent.LoadConfig(f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Updates == nil || !config.Updates.Enabled || config.Updates.NodeID != "node-id" {
		t.Fatal(config.Updates)
	}
	after, err := os.ReadFile(f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var oldJSON, newJSON map[string]json.RawMessage
	if err = json.Unmarshal(before, &oldJSON); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(after, &newJSON); err != nil {
		t.Fatal(err)
	}
	delete(newJSON, "updates")
	if !reflect.DeepEqual(oldJSON, newJSON) {
		t.Fatal("unrelated config values changed")
	}
	assertMode(t, installer.helperBinaryPath(), executableFile)
	assertMode(t, installer.helperUnitPath(), privateFile)
	runtimeConfig, err := installer.UpdateConfiguration(t.Context(), f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeConfig.Installed.Version != "v1.0.0" || runtimeConfig.NodeID != "node-id" {
		t.Fatal(runtimeConfig.Installed)
	}
	updater, err := installer.BuildUpdater(t.Context(), f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = updater.VerifyRunningImage(t.Context()); err == nil {
		t.Fatal("test process claimed Agent identity")
	}
	if _, err = installer.BuildUpdateHelper(t.Context(), f.options.ConfigPath); err == nil {
		t.Fatal("test process claimed helper identity")
	}
	manager, err := installer.UpdateManager(t.Context(), f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Kind() != "systemd" {
		t.Fatal(manager.Kind())
	}
	if err = manager.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = installer.EnableUpdates(t.Context(), f.options); err != nil {
		t.Fatal("repeat enable", err)
	}
}

func TestEnableUpdatesRejectsUntrustedPrerequisites(t *testing.T) {
	for _, kind := range []string{"node", "http", "manifest", "trust", "artifact", "binary", "container", "unit", "helper-unit", "write", "command", "running-image", "running-version"} {
		t.Run(kind, func(t *testing.T) {
			f := newUpdateFixture(t)
			installer := f.base.installer
			switch kind {
			case "running-version":
				installer.version = func() string { return "v9.0.0" }
			case "running-image":
				source, err := installer.executable()
				if err != nil {
					t.Fatal(err)
				}
				mustWrite(t, source, []byte("other current CLI"), executableFile)
			case "node":
				f.options.NodeID = ""
			case "http":
				f.base.result.Config.ServerURL = "http://beat.test"
				writeUpdateJSON(t, f.options.ConfigPath, f.base.result.Config)
			case "manifest":
				mustWrite(t, f.options.ManifestPath, []byte("{}"), privateFile)
			case "trust":
				mustWrite(t, f.options.TrustPath, []byte("[]"), privateFile)
			case "artifact":
				f.options.ArtifactID = "other"
			case "binary":
				mustWrite(t, installer.paths.binary, []byte("unknown binary"), executableFile)
			case "container":
				installer.container = func() bool { return true }
			case "unit":
				mustWrite(t, installer.paths.unit, []byte("unmanaged"), privateFile)
			case "helper-unit":
				mustWrite(t, installer.helperUnitPath(), []byte("unmanaged"), privateFile)
			case "write":
				installer.write = func(string, []byte, os.FileMode) error { return errors.New("write failed") }
			case "command":
				f.base.failCommand = "daemon-reload"
			}
			if err := installer.EnableUpdates(t.Context(), f.options); err == nil {
				t.Fatal("untrusted update enable accepted")
			}
		})
	}
}

func TestUpdateNodeIDUsesExistingEnrollment(t *testing.T) {
	f := newUpdateFixture(t)
	installer := f.base.installer
	config, err := agent.LoadConfig(f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	options := JoinOptions{ServerURL: config.ServerURL, Name: config.NodeName, Host: config.AdvertisedHost, SSHPort: config.SSHPort}
	state, err := installer.createEnrollment(options)
	if err != nil {
		t.Fatal(err)
	}
	state.Result = &f.base.result
	if err = installer.saveEnrollment(state); err != nil {
		t.Fatal(err)
	}
	nodeID, err := installer.updateNodeID(config, "")
	if err != nil || nodeID != "node-id" {
		t.Fatal(nodeID, err)
	}
	if _, err = installer.updateNodeID(config, "another-node"); err == nil {
		t.Fatal("identity changed")
	}
	config.AgentToken = "different"
	if _, err = installer.updateNodeID(config, ""); err == nil {
		t.Fatal("credential conflict ignored")
	}
}

func TestUpdateManagerAndRuntimeAdmission(t *testing.T) {
	f := newUpdateFixture(t)
	installer := f.base.installer
	if _, err := installer.UpdateConfiguration(t.Context(), f.options.ConfigPath); !errors.Is(err, agentupdate.ErrDisabled) {
		t.Fatal(err)
	}
	if _, err := installer.UpdateConfiguration(t.Context(), "/missing"); err == nil {
		t.Fatal("missing config accepted")
	}
	manager, err := installer.UpdateManager(t.Context(), f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.(*updateManager).Ready(t.Context()); !errors.Is(err, agentupdate.ErrManualOnly) {
		t.Fatal("missing watchdog accepted", err)
	}
	installer.run = func(context.Context, string, []string) (string, error) {
		return "FragmentPath=/foreign\nDropInPaths=\nLoadState=loaded\n", nil
	}
	if err = manager.Restart(t.Context()); !errors.Is(err, agentupdate.ErrManualOnly) {
		t.Fatal(err)
	}
	unit := string(helperUnitFile("/private/path", "/private/config"))
	if strings.Contains(unit, "PartOf=") || strings.Contains(unit, "BindsTo=") || !strings.Contains(unit, "Restart=always") {
		t.Fatal("watchdog tied to Agent lifetime")
	}
	installer.goos = "windows"
	if _, err = installer.UpdateManager(t.Context(), f.options.ConfigPath); !errors.Is(err, agentupdate.ErrManualOnly) {
		t.Fatal(err)
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "fixture")
	if !detectUpdateContainer() {
		t.Fatal("container not detected")
	}
}

func TestFixedManagerReadsBoundedAutomaticRestartCount(t *testing.T) {
	f := newUpdateFixture(t)
	installer := f.base.installer
	manager, err := installer.UpdateManager(t.Context(), f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := installer.run
	for _, output := range []string{"2", "-1", "not-a-count"} {
		installer.run = func(ctx context.Context, exe string, args []string) (string, error) {
			for _, arg := range args {
				if arg == "--property=NRestarts" {
					return output, nil
				}
			}
			return previous(ctx, exe, args)
		}
		count, err := manager.(*updateManager).AutomaticRestarts(t.Context())
		if output == "2" {
			if err != nil || count != 2 {
				t.Fatal(count, err)
			}
		} else if err == nil {
			t.Fatal("invalid restart counter accepted")
		}
	}
}

func TestReadinessRequiresUnmodifiedActiveHelper(t *testing.T) {
	f := newUpdateFixture(t)
	installer := f.base.installer
	if err := installer.EnableUpdates(t.Context(), f.options); err != nil {
		t.Fatal(err)
	}
	manager, err := installer.UpdateManager(t.Context(), f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, installer.helperUnitPath(), []byte("unmanaged helper"), privateFile)
	if err = manager.(*updateManager).Ready(t.Context()); !errors.Is(err, agentupdate.ErrManualOnly) {
		t.Fatal(err)
	}
	mustWrite(t, installer.helperUnitPath(), helperUnitFile(installer.helperBinaryPath(), f.options.ConfigPath), privateFile)
	previous := installer.run
	installer.run = func(ctx context.Context, exe string, args []string) (string, error) {
		if args[1] == "is-active" {
			return "", errors.New("helper inactive")
		}
		return previous(ctx, exe, args)
	}
	if err = manager.(*updateManager).Ready(t.Context()); !errors.Is(err, agentupdate.ErrManualOnly) {
		t.Fatal(err)
	}
	installer.version = NewInstaller().version
	if err = installer.matchRunningBootstrap(agentupdate.BootstrapOptions{}); err == nil {
		t.Fatal("empty manifest accepted")
	}
	if installer.version() == "" {
		t.Fatal("missing compiled version")
	}
	installer.version = nil
	if installer.matchesRunningVersion("v1.0.0") {
		t.Fatal("missing version provider accepted")
	}
}
