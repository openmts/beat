//go:build linux

package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/agentupdate"
)

// writeUpdatesConfig 把夹具里的有效配置补上 updates 段写到新路径，用于制造「已启用更新但信任库缺失」。
func writeUpdatesConfig(t *testing.T, base, target string) {
	t.Helper()
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["updates"] = map[string]any{"enabled": true, "node_id": "node-id"}
	patched, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, target, patched, privateFile)
}

// loadedUnitOutput 复刻夹具里 systemctl show 的输出，按单元名给出对应路径。
func loadedUnitOutput(f *updateFixture, args []string) string {
	path := f.base.installer.paths.unit
	if len(args) > 2 && args[2] == UpdateServiceName {
		path = f.base.installer.helperUnitPath()
	}
	return "FragmentPath=" + path + "\nDropInPaths=\nLoadState=loaded\n"
}

// updateManagerFor 返回夹具里可用的具体管理器，用于触达只在具体类型上暴露的失败分支。
func updateManagerFor(t *testing.T, f *updateFixture) (*updateManager, error) {
	t.Helper()
	manager, err := f.base.installer.UpdateManager(t.Context(), f.options.ConfigPath)
	if err != nil {
		return nil, err
	}
	concrete, ok := manager.(*updateManager)
	if !ok {
		t.Fatalf("unexpected manager type %T", manager)
	}
	return concrete, nil
}

func TestUpdateConfigurationFailureModes(t *testing.T) {
	t.Run("external manager", func(t *testing.T) {
		f := newUpdateFixture(t)
		path := filepath.Join(t.TempDir(), "agent.json")
		writeUpdatesConfig(t, f.base.installer.paths.config, path)
		mustWrite(t, f.base.installer.paths.unit, unitFile(f.base.installer.paths.binary, path), privateFile)
		f.base.installer.container = func() bool { return true }
		_, err := f.base.installer.UpdateConfiguration(t.Context(), path)
		if !errors.Is(err, agentupdate.ErrExternalManager) {
			t.Fatalf("expected external manager refusal, got %v", err)
		}
	})
	t.Run("missing trust store", func(t *testing.T) {
		f := newUpdateFixture(t)
		path := filepath.Join(t.TempDir(), "agent.json")
		writeUpdatesConfig(t, f.base.installer.paths.config, path)
		mustWrite(t, f.base.installer.paths.unit, unitFile(f.base.installer.paths.binary, path), privateFile)
		_, err := f.base.installer.UpdateConfiguration(t.Context(), path)
		if !errors.Is(err, agentupdate.ErrManualOnly) {
			t.Fatalf("expected manual-only refusal, got %v", err)
		}
	})
	t.Run("missing receipt", func(t *testing.T) {
		f := newUpdateFixture(t)
		if err := f.base.installer.EnableUpdates(t.Context(), f.options); err != nil {
			t.Fatal(err)
		}
		state := filepath.Join(filepath.Dir(f.base.installer.paths.binary), ".beat-agent-update")
		if err := os.Remove(filepath.Join(state, "installed.json")); err != nil {
			t.Fatal(err)
		}
		_, err := f.base.installer.UpdateConfiguration(t.Context(), f.options.ConfigPath)
		if !errors.Is(err, agentupdate.ErrManualOnly) {
			t.Fatalf("expected manual-only refusal, got %v", err)
		}
	})
}

func TestBuildUpdaterFailureModes(t *testing.T) {
	t.Run("missing configuration", func(t *testing.T) {
		f := newUpdateFixture(t)
		if _, err := f.base.installer.BuildUpdater(t.Context(), filepath.Join(t.TempDir(), "missing.json")); err == nil {
			t.Fatal("missing configuration accepted")
		}
	})
	t.Run("missing helper unit", func(t *testing.T) {
		f := newUpdateFixture(t)
		if err := f.base.installer.EnableUpdates(t.Context(), f.options); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(f.base.installer.helperUnitPath()); err != nil {
			t.Fatal(err)
		}
		_, err := f.base.installer.BuildUpdater(t.Context(), f.options.ConfigPath)
		if !errors.Is(err, agentupdate.ErrManualOnly) {
			t.Fatalf("expected manual-only refusal, got %v", err)
		}
	})
	t.Run("corrupted helper image", func(t *testing.T) {
		f := newUpdateFixture(t)
		if err := f.base.installer.EnableUpdates(t.Context(), f.options); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, f.base.installer.helperBinaryPath(), []byte("corrupted helper image"), executableFile)
		if _, err := f.base.installer.BuildUpdater(t.Context(), f.options.ConfigPath); err == nil {
			t.Fatal("corrupted helper image accepted")
		}
	})
}

func TestBuildUpdateHelperRejectsMissingConfiguration(t *testing.T) {
	f := newUpdateFixture(t)
	if _, err := f.base.installer.BuildUpdateHelper(t.Context(), filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing configuration accepted")
	}
}

func TestUpdateManagerFailureModes(t *testing.T) {
	t.Run("unsafe config path", func(t *testing.T) {
		f := newUpdateFixture(t)
		_, err := f.base.installer.UpdateManager(t.Context(), filepath.Join(t.TempDir(), "missing.json"))
		if !errors.Is(err, agentupdate.ErrManualOnly) {
			t.Fatalf("expected manual-only refusal, got %v", err)
		}
	})
	t.Run("unqueryable unit", func(t *testing.T) {
		f := newUpdateFixture(t)
		manager, err := updateManagerFor(t, f)
		if err != nil {
			t.Fatal(err)
		}
		f.base.installer.run = func(context.Context, string, []string) (string, error) {
			return "", errors.New("systemd unavailable")
		}
		if _, err = manager.AutomaticRestarts(t.Context()); !errors.Is(err, agentupdate.ErrManualOnly) {
			t.Fatalf("expected manual-only refusal, got %v", err)
		}
	})
	t.Run("unavailable restart counter", func(t *testing.T) {
		f := newUpdateFixture(t)
		manager, err := updateManagerFor(t, f)
		if err != nil {
			t.Fatal(err)
		}
		f.base.installer.run = func(_ context.Context, _ string, args []string) (string, error) {
			if strings.Contains(strings.Join(args, " "), "--property=NRestarts") {
				return "", errors.New("systemd unavailable")
			}
			return loadedUnitOutput(f, args), nil
		}
		if _, err = manager.AutomaticRestarts(t.Context()); !errors.Is(err, agentupdate.ErrManager) {
			t.Fatalf("expected manager failure, got %v", err)
		}
	})
	t.Run("configured helper unit path", func(t *testing.T) {
		f := newUpdateFixture(t)
		f.base.installer.paths.helperUnit = filepath.Join(t.TempDir(), UpdateServiceName)
		if got := f.base.installer.helperUnitPath(); got != f.base.installer.paths.helperUnit {
			t.Fatalf("expected configured helper unit path, got %s", got)
		}
	})
}
