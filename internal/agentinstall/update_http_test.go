//go:build linux

package agentinstall

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentupdate"
)

func TestHTTPMonitoringConfigCannotEnableUpdatesOrMutateInstallation(t *testing.T) {
	f := newUpdateFixture(t)
	f.base.result.Config.ServerURL = "http://127.0.0.1:1"
	writeUpdateJSON(t, f.options.ConfigPath, f.base.result.Config)
	before, err := os.ReadFile(f.options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	err = f.base.installer.EnableUpdates(t.Context(), f.options)
	if !errors.Is(err, agentupdate.ErrConfiguration) {
		t.Fatal("HTTP update admission was not rejected", err)
	}
	after, err := os.ReadFile(f.options.ConfigPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("monitoring config changed", err)
	}
	installed, err := os.ReadFile(f.base.installer.paths.binary)
	if err != nil || !bytes.Equal(f.data, installed) {
		t.Fatal("installed Agent changed", err)
	}
	for _, path := range []string{f.base.installer.helperBinaryPath(), f.base.installer.helperUnitPath(), filepath.Join(filepath.Dir(f.base.installer.paths.binary), ".beat-agent-update")} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("HTTP refusal created update state", path, err)
		}
	}
	for _, call := range f.base.calls {
		for _, arg := range call {
			if arg == "enable" || arg == "restart" || arg == "daemon-reload" {
				t.Fatal("HTTP refusal changed services", call)
			}
		}
	}
	config, err := agent.LoadConfig(f.options.ConfigPath)
	if err != nil || config.Updates != nil {
		t.Fatal("normal monitoring config no longer loads", err)
	}
	if _, err = agent.NewHTTPReporter(config); err != nil {
		t.Fatal("normal HTTP monitoring was disabled", err)
	}
}
