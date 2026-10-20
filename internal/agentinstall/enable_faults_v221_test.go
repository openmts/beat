//go:build linux

package agentinstall

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/agentjoin"
	"github.com/beat/backend/internal/agentupdate"
)

func TestEnableUpdatesUsesDefaultConfigPath(t *testing.T) {
	f := newUpdateFixture(t)
	options := f.options
	options.ConfigPath = ""
	if err := f.base.installer.EnableUpdates(t.Context(), options); err != nil {
		t.Fatal(err)
	}
}

func TestEnableUpdatesRejectsBrokenInputs(t *testing.T) {
	stateFile := func(f *updateFixture, name string) string {
		return filepath.Join(f.base.installer.paths.stateDir, name)
	}
	cases := map[string]func(*updateFixture){
		"missing config": func(f *updateFixture) {
			f.options.ConfigPath = stateFile(f, "missing.json")
		},
		"missing manifest": func(f *updateFixture) { f.options.ManifestPath = stateFile(f, "missing.json") },
		"missing trust":    func(f *updateFixture) { f.options.TrustPath = stateFile(f, "missing.json") },
		"corrupt manifest": func(f *updateFixture) { mustWrite(t, f.options.ManifestPath, []byte("{"), privateFile) },
		"corrupt trust":    func(f *updateFixture) { mustWrite(t, f.options.TrustPath, []byte("{"), privateFile) },
		"corrupt enrollment state": func(f *updateFixture) {
			mustWrite(t, stateFile(f, "enrollment.json"), []byte("{"), privateFile)
		},
		"rejected enrollment result": func(f *updateFixture) {
			writeUpdateJSON(t, stateFile(f, "enrollment.json"),
				enrollment{Version: stateVersion, ServerURL: "https://beat.test", Result: &agentjoin.Result{}})
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			f := newUpdateFixture(t)
			prepare(f)
			if err := f.base.installer.EnableUpdates(t.Context(), f.options); err == nil {
				t.Fatal("broken update enable accepted")
			}
		})
	}
}

func TestStartUpdateHelperRefusesUnmanagedState(t *testing.T) {
	t.Run("unit path is a directory", func(t *testing.T) {
		f := newUpdateFixture(t)
		mustMkdir(t, f.base.installer.helperUnitPath())
		if err := f.base.installer.startUpdateHelper(t.Context(), f.options.ConfigPath); err == nil {
			t.Fatal("directory in place of helper unit accepted")
		}
	})
	t.Run("unit query failure", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.installer.run = func(context.Context, string, []string) (string, error) {
			return "", errors.New("systemd unavailable")
		}
		if err := f.base.installer.startUpdateHelper(t.Context(), f.options.ConfigPath); err == nil {
			t.Fatal("failed unit query accepted")
		}
	})
	t.Run("unit bound elsewhere", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.installer.run = func(context.Context, string, []string) (string, error) {
			return "FragmentPath=" + f.base.installer.paths.unit + "\nDropInPaths=\nLoadState=loaded\n", nil
		}
		if err := f.base.installer.startUpdateHelper(t.Context(), f.options.ConfigPath); err == nil {
			t.Fatal("helper unit from another location accepted")
		}
	})
	t.Run("enable failure", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.failCommand = "enable"
		if err := f.base.installer.EnableUpdates(t.Context(), f.options); err == nil {
			t.Fatal("failed enable accepted")
		}
	})
	t.Run("restart failure", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.failCommand = "restart"
		if err := f.base.installer.EnableUpdates(t.Context(), f.options); err == nil {
			t.Fatal("failed restart accepted")
		}
	})
}

func TestEnableUpdateSettingsRejectsUnreadableConfig(t *testing.T) {
	f := newUpdateFixture(t)
	settings := agentupdate.Settings{Enabled: true, NodeID: "node-id"}
	if err := f.base.installer.enableUpdateSettings(filepath.Join(t.TempDir(), "missing.json"), settings); err == nil {
		t.Fatal("missing config accepted")
	}
	broken := filepath.Join(t.TempDir(), "agent.json")
	mustWrite(t, broken, []byte("{"), privateFile)
	if err := f.base.installer.enableUpdateSettings(broken, settings); err == nil {
		t.Fatal("invalid config accepted")
	}
}

func TestMatchRunningBootstrapRequiresReadableImage(t *testing.T) {
	t.Run("missing current executable", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.installer.executable = func() (string, error) { return "", errors.New("no current CLI") }
		if err := f.base.installer.matchRunningBootstrap(bootstrapOptions(t, f)); err == nil {
			t.Fatal("missing current executable accepted")
		}
	})
	t.Run("unreadable image", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.installer.executable = func() (string, error) { return filepath.Join(t.TempDir(), "gone"), nil }
		if err := f.base.installer.matchRunningBootstrap(bootstrapOptions(t, f)); err == nil {
			t.Fatal("unreadable image accepted")
		}
	})
}

func TestGrantHelpersFillDefaults(t *testing.T) {
	f := newFixture(t)
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatal(err)
	}
	options, err := f.installer.normalizeGrantOptions(GrantOptions{ServerURL: "https://beat.test",
		GrantSecret: secret.Plaintext, Name: "lab-1", Host: "192.0.2.8", SSHPort: 22})
	if err != nil {
		t.Fatal(err)
	}
	if options.PollTimeout != defaultGrantPollTimeout || options.PollInterval != defaultGrantPollInterval {
		t.Fatalf("unexpected poll defaults: %+v", options)
	}
	config := grantConfig(grantClaim{ServerURL: "https://beat.test", AgentToken: "token", NodeName: "lab-1", Host: "192.0.2.8", Port: 22})
	if config.ReportInterval != agentjoin.DefaultReportInterval {
		t.Fatalf("unexpected report interval %q", config.ReportInterval)
	}
}

func TestCheckGrantConfigRejectsUnsafeState(t *testing.T) {
	t.Run("unreadable path", func(t *testing.T) {
		f := newFixture(t)
		f.installer.paths.config = filepath.Join(t.TempDir(), "config-directory")
		mustMkdir(t, f.installer.paths.config)
		if err := f.installer.checkGrantConfig(agentjoin.Config{}); err == nil {
			t.Fatal("directory in place of config accepted")
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		f := newFixture(t)
		mustMkdir(t, filepath.Dir(f.installer.paths.config))
		mustWrite(t, f.installer.paths.config, []byte("{"), privateFile)
		if err := f.installer.checkGrantConfig(agentjoin.Config{}); err == nil {
			t.Fatal("invalid config accepted")
		}
	})
}

// bootstrapOptions 返回与夹具签名清单一致的引导选项，用于单独驱动镜像匹配检查。
func bootstrapOptions(t *testing.T, f *updateFixture) agentupdate.BootstrapOptions {
	t.Helper()
	return agentupdate.BootstrapOptions{ArtifactID: "linux", Manifest: signedUpdate(t, f.key, f.data, "v1.0.0", time.Now().UTC())}
}
