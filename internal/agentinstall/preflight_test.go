package agentinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/agentcredential"
)

func TestJoinPreflightRejectsUnsupportedMachine(t *testing.T) {
	cases := map[string]func(*fixture){
		"OS":      func(f *fixture) { f.installer.goos = "darwin" },
		"user":    func(f *fixture) { f.installer.euid = func() int { return 1000 } },
		"systemd": func(f *fixture) { f.installer.paths.systemd += "/missing" },
		"systemctl": func(f *fixture) {
			f.installer.lookPath = func(string) (string, error) { return "", errors.New("missing") }
		},
		"manager": func(f *fixture) { f.failCommand = "show" },
		"executable": func(f *fixture) {
			f.installer.executable = func() (string, error) { return "", errors.New("unavailable") }
		},
		"missing executable": func(f *fixture) { f.installer.executable = func() (string, error) { return "/not-an-agent", nil } },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mutate(f)
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected preflight failure")
			}
			if f.requests.Load() != 0 {
				t.Fatal("preflight failure consumed a token")
			}
		})
	}
}

func TestJoinRejectsInvalidOptionsBeforeNetwork(t *testing.T) {
	cases := map[string]func(*fixture){
		"http":          func(f *fixture) { f.options.AllowHTTP = false },
		"url":           func(f *fixture) { f.options.ServerURL += "/api" },
		"token":         func(f *fixture) { f.options.Token = "invalid" },
		"name":          func(f *fixture) { f.options.Name = "\x00" },
		"host":          func(f *fixture) { f.options.Host = "invalid/host" },
		"port":          func(f *fixture) { f.options.SSHPort = -1 },
		"hostname":      func(f *fixture) { f.installer.hostname = func() (string, error) { return "", errors.New("failed") } },
		"SSH detection": func(f *fixture) { f.installer.sshPort = func() (int, error) { return 0, errors.New("failed") } },
		"randomness": func(f *fixture) {
			f.installer.credential = func() (agentcredential.Token, error) { return agentcredential.Token{}, errors.New("random failed") }
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mutate(f)
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected options failure")
			}
			if f.requests.Load() != 0 {
				t.Fatal("invalid options consumed a token")
			}
		})
	}
}

func TestJoinDoesNotOverwriteExistingFiles(t *testing.T) {
	for _, kind := range []string{"config", "unit", "binary", "lock", "config symlink", "directory symlink", "directory mode", "unit mode"} {
		t.Run(kind, func(t *testing.T) { testFileConflict(t, kind) })
	}
}

func testFileConflict(t *testing.T, kind string) {
	f := newFixture(t)
	paths := f.installer.paths
	mustMkdir(t, paths.stateDir)
	mustMkdir(t, filepath.Dir(paths.binary))
	mustMkdir(t, filepath.Dir(paths.unit))
	switch kind {
	case "config":
		mustWrite(t, paths.config, []byte(`{}`), privateFile)
	case "unit":
		mustWrite(t, paths.unit, []byte("[Service]\nExecStart=/other"), privateFile)
	case "binary":
		mustWrite(t, paths.binary, []byte("unmanaged"), executableFile)
	case "lock":
		mustMkdir(t, filepath.Join(paths.stateDir, "installation.lock"))
	case "config symlink":
		if err := os.Symlink(paths.binary, paths.config); err != nil {
			t.Fatal(err)
		}
	case "directory symlink":
		f.installer.paths.stateDir = filepath.Join(filepath.Dir(paths.stateDir), "state-link")
		if err := os.Symlink(paths.stateDir, f.installer.paths.stateDir); err != nil {
			t.Fatal(err)
		}
	case "directory mode":
		if err := os.Chmod(paths.stateDir, 0o755); err != nil {
			t.Fatal(err)
		}
	case "unit mode":
		mustWrite(t, paths.unit, unitFile(paths.binary, paths.config), 0o644)
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected conflict error")
	}
	if f.requests.Load() != 0 {
		t.Fatal("unsafe local state consumed a token")
	}
}

func TestJoinRejectsConcurrentInstallation(t *testing.T) {
	f := newFixture(t)
	release, err := f.installer.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected concurrent installation lock")
	}
	if f.requests.Load() != 0 {
		t.Fatal("concurrent join consumed a token")
	}
}

func TestJoinRejectsSystemdOverrides(t *testing.T) {
	for _, output := range []string{"invalid output", "FragmentPath=/usr/lib/systemd/system/beat-agent.service\nDropInPaths=\n", "FragmentPath=\nDropInPaths=/etc/override.conf\n"} {
		t.Run(output, func(t *testing.T) {
			f := newFixture(t)
			f.installer.run = func(context.Context, string, []string) (string, error) { return output, nil }
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected unknown service rejection")
			}
			if f.requests.Load() != 0 {
				t.Fatal("systemd override consumed a token")
			}
		})
	}
}
