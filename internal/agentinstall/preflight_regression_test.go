package agentinstall

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinPreservesInvalidInstallationRecords(t *testing.T) {
	for _, kind := range []string{"truncated JSON", "unsupported version", "another config"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			mustMkdir(t, f.installer.paths.stateDir)
			path := filepath.Join(f.installer.paths.stateDir, "installation.json")
			record := installation{Version: stateVersion, ConfigPath: f.installer.paths.config}
			if kind == "unsupported version" {
				record.Version++
			}
			if kind == "another config" {
				record.ConfigPath = filepath.Join(f.installer.paths.stateDir, "other.json")
			}
			if err := f.installer.saveJSON(path, record); err != nil {
				t.Fatal(err)
			}
			if kind == "truncated JSON" {
				mustWrite(t, path, []byte(`{"version":`), privateFile)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("invalid installation record must block enrollment")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || f.requests.Load() != 0 {
				t.Fatal("invalid record was overwritten or a token was consumed")
			}
		})
	}
}

func TestJoinRejectsSymlinkAtStableBinaryPath(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Dir(f.installer.paths.binary))
	source, err := f.installer.executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, f.installer.paths.binary); err != nil {
		t.Fatal(err)
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("symlinked stable binary must block enrollment")
	}
	info, err := os.Lstat(f.installer.paths.binary)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 || f.requests.Load() != 0 {
		t.Fatal("symlink was replaced or token was consumed")
	}
}

func TestJoinRejectsUnitDirectoryConflicts(t *testing.T) {
	f := newFixture(t)
	source, err := f.installer.executable()
	if err != nil {
		t.Fatal(err)
	}
	f.installer.paths.unit = filepath.Join(source, ServiceName)
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("a regular file cannot become the systemd unit directory")
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || f.requests.Load() != 0 {
		t.Fatal("preflight changed the conflicting file or consumed a token")
	}
}

func TestJoinRejectsAmbiguousSystemdProperties(t *testing.T) {
	f := newFixture(t)
	f.installer.run = func(context.Context, string, []string) (string, error) {
		return "LoadState=not-found\nFragmentPath=\nDropInPaths=\nDropInPaths=/unexpected-override\n", nil
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("expected systemd validation error, got %v", err)
	}
	if f.requests.Load() != 0 {
		t.Fatal("ambiguous systemd state consumed a token")
	}
}

func TestJoinPreservesMalformedExistingConfig(t *testing.T) {
	f := newFixture(t)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	broken := []byte(`{"server_url":`)
	mustWrite(t, f.installer.paths.config, broken, privateFile)
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("malformed existing config must be preserved for operator recovery")
	}
	actual, err := os.ReadFile(f.installer.paths.config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(broken, actual) || f.requests.Load() != 1 {
		t.Fatal("config was replaced or another enrollment was attempted")
	}
}

func TestJoinCopiesItsRunningImageWithDefaultSource(t *testing.T) {
	f := newFixture(t)
	f.installer.executable = NewInstaller().executable
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(f.installer.paths.binary)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatal("stable executable differs from the currently running image")
	}
	f.assertInstalled(t)
}
