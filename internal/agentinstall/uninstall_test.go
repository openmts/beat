package agentinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unitRunner 让 show 只认识 states 里的单元，其余查询一律按 not-found 处理。
func unitRunner(f *fixture, states map[string]string) {
	f.installer.run = func(_ context.Context, _ string, args []string) (string, error) {
		f.calls = append(f.calls, append([]string(nil), args...))
		if args[1] == f.failCommand {
			return "", errors.New("test systemctl failure")
		}
		if args[1] != "show" {
			return "", nil
		}
		path, ok := states[args[2]]
		if !ok {
			return "FragmentPath=\nDropInPaths=\nLoadState=not-found\n", nil
		}
		return "FragmentPath=" + path + "\nDropInPaths=\nLoadState=loaded\n", nil
	}
}

func installedFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	return f
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("want %s removed, got %v", path, err)
	}
}

func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("want %s kept: %v", path, err)
	}
}

func TestServiceUninstallRemovesUnitsAndBinaries(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	summary, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.installer.paths.unit, f.installer.paths.binary} {
		assertMissing(t, path)
		if !containsPath(summary.Removed, path) {
			t.Fatalf("summary does not mention %s: %+v", path, summary)
		}
	}
	assertPresent(t, f.installer.paths.config)
	if !containsPath(summary.Retained, f.installer.paths.stateDir) {
		t.Fatalf("config directory should be retained: %+v", summary)
	}
	for _, call := range []string{"disable --now " + ServiceName, "daemon-reload"} {
		if !hasCall(f.calls, call) {
			t.Errorf("missing systemctl %s in %v", call, f.calls)
		}
	}
}

func TestServiceUninstallPurgesStateWhenEmpty(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	mustWrite(t, filepath.Join(f.installer.paths.stateDir, "identity.json"), []byte("{}\n"), privateFile)
	summary, err := f.installer.Uninstall(context.Background(), UninstallOptions{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	assertMissing(t, f.installer.paths.stateDir)
	if !containsPath(summary.Removed, filepath.Join(f.installer.paths.stateDir, "identity.json")) {
		t.Fatalf("purge should report the identity file: %+v", summary)
	}
	if len(summary.Retained) != 0 {
		t.Fatalf("purge should retain nothing: %+v", summary)
	}
}

func TestServiceUninstallKeepsUnknownStateFiles(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	notes := filepath.Join(f.installer.paths.stateDir, "operator-notes.txt")
	mustWrite(t, notes, []byte("keep me\n"), privateFile)
	summary, err := f.installer.Uninstall(context.Background(), UninstallOptions{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	assertPresent(t, notes)
	if containsPath(summary.Removed, notes) {
		t.Fatalf("purge must not touch unknown files: %+v", summary)
	}
	if !containsPath(summary.Retained, f.installer.paths.stateDir) {
		t.Fatalf("non-empty state directory should be retained: %+v", summary)
	}
}

func TestServiceUninstallRefusesUnmanagedUnit(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	mustWrite(t, f.installer.paths.unit, []byte("[Unit]\nDescription=vendor unit\n"), privateFile)
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "not managed by beat-agent") {
		t.Fatalf("want unmanaged unit error, got %v", err)
	}
	assertPresent(t, f.installer.paths.unit)
	assertPresent(t, f.installer.paths.binary)
}

func TestServiceUninstallReportsNotInstalled(t *testing.T) {
	f := newFixture(t)
	if _, err := f.installer.Uninstall(context.Background(), UninstallOptions{Purge: true}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("want ErrNotInstalled, got %v", err)
	}
}

func TestServiceUninstallStopsOnSystemctlFailure(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	f.failCommand = "disable"
	if _, err := f.installer.Uninstall(context.Background(), UninstallOptions{}); err == nil {
		t.Fatal("expected systemctl failure")
	}
	assertPresent(t, f.installer.paths.unit)
	assertPresent(t, f.installer.paths.binary)
}

func TestServiceUninstallRemovesUpdateHelper(t *testing.T) {
	f := installedFixture(t)
	state := filepath.Join(filepath.Dir(f.installer.paths.binary), updateStateDirectory)
	mustMkdir(t, state)
	mustWrite(t, f.installer.helperUnitPath(), helperUnitFile(f.installer.helperBinaryPath(), f.installer.paths.config), privateFile)
	mustWrite(t, f.installer.helperBinaryPath(), []byte("helper"), executableFile)
	unitRunner(f, map[string]string{
		ServiceName:       f.installer.paths.unit,
		UpdateServiceName: f.installer.helperUnitPath(),
	})
	if _, err := f.installer.Uninstall(context.Background(), UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{f.installer.helperUnitPath(), f.installer.helperBinaryPath(), state} {
		assertMissing(t, path)
	}
	if !hasCall(f.calls, "disable --now "+UpdateServiceName) {
		t.Errorf("update helper service was not disabled: %v", f.calls)
	}
}

func TestServiceUninstallRejectsUnsafeUpdateState(t *testing.T) {
	f := installedFixture(t)
	state := filepath.Join(filepath.Dir(f.installer.paths.binary), updateStateDirectory)
	mustMkdir(t, state)
	if err := os.Chmod(state, 0o755); err != nil {
		t.Fatal(err)
	}
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe update state directory") {
		t.Fatalf("want unsafe directory error, got %v", err)
	}
}

func TestServiceUninstallPropagatesCancellation(t *testing.T) {
	f := installedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.installer.Uninstall(ctx, UninstallOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
	assertPresent(t, f.installer.paths.unit)
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func hasCall(calls [][]string, want string) bool {
	for _, args := range calls {
		if strings.Join(args[1:], " ") == want {
			return true
		}
	}
	return false
}

func TestServiceUninstallToleratesMissingSystemdUnits(t *testing.T) {
	f := installedFixture(t)
	f.installer.run = func(_ context.Context, _ string, args []string) (string, error) {
		f.calls = append(f.calls, append([]string(nil), args...))
		if args[1] == "show" {
			return "FragmentPath=\nDropInPaths=\nLoadState=not-found\n", errors.New("systemctl show failed")
		}
		return "", nil
	}
	if _, err := f.installer.Uninstall(context.Background(), UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, f.installer.paths.unit)
}

func TestServiceUninstallRejectsUnknownSystemdProperties(t *testing.T) {
	f := installedFixture(t)
	f.installer.run = func(_ context.Context, _ string, args []string) (string, error) {
		f.calls = append(f.calls, append([]string(nil), args...))
		if args[1] == "show" {
			return "FragmentPath=/etc/systemd/system/beat-agent.service\nDropInPaths=\n", nil
		}
		return "", nil
	}
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unknown systemd overrides") {
		t.Fatalf("want override error, got %v", err)
	}
	assertPresent(t, f.installer.paths.unit)
}

func TestServiceUninstallRejectsUnsafeUnitFile(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	if err := os.Chmod(f.installer.paths.unit, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe file") {
		t.Fatalf("want unsafe unit error, got %v", err)
	}
	assertPresent(t, f.installer.paths.unit)
}

func TestServiceUninstallRejectsUnsafeBinary(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	if err := os.Chmod(f.installer.paths.binary, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe file") {
		t.Fatalf("want unsafe binary error, got %v", err)
	}
	assertPresent(t, f.installer.paths.binary)
}

func TestServiceUninstallKeepsBinaryDirectoryWithForeignFiles(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	foreign := filepath.Join(filepath.Dir(f.installer.paths.binary), "operator.txt")
	mustWrite(t, foreign, []byte("keep\n"), privateFile)
	summary, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertPresent(t, foreign)
	if containsPath(summary.Removed, filepath.Dir(f.installer.paths.binary)) {
		t.Fatalf("binary directory with foreign files must be kept: %+v", summary)
	}
}

func TestServiceUninstallSurfacesDaemonReloadFailure(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	f.failCommand = "daemon-reload"
	if _, err := f.installer.Uninstall(context.Background(), UninstallOptions{}); err == nil {
		t.Fatal("expected daemon-reload failure")
	}
}

func TestServiceUninstallRejectsUnsafeStateDirectory(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Dir(f.installer.paths.binary))
	mustWrite(t, f.installer.paths.binary, []byte("agent"), executableFile)
	mustMkdir(t, filepath.Dir(f.installer.paths.stateDir))
	mustWrite(t, f.installer.paths.stateDir, []byte("not a directory\n"), privateFile)
	unitRunner(f, nil)
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsafe agent state directory") {
		t.Fatalf("want unsafe state directory error, got %v", err)
	}
}

func TestServiceUninstallPurgesWithoutStateDirectory(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Dir(f.installer.paths.binary))
	mustWrite(t, f.installer.paths.binary, []byte("agent"), executableFile)
	unitRunner(f, nil)
	summary, err := f.installer.Uninstall(context.Background(), UninstallOptions{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	assertMissing(t, f.installer.paths.binary)
	if !containsPath(summary.Retained, f.installer.paths.stateDir) {
		t.Fatalf("missing state directory should be reported as retained: %+v", summary)
	}
}

func TestServiceUninstallSurfacesSystemdInspectionFailure(t *testing.T) {
	f := installedFixture(t)
	f.installer.run = func(_ context.Context, _ string, args []string) (string, error) {
		f.calls = append(f.calls, append([]string(nil), args...))
		if args[1] == "show" {
			return "", errors.New("systemctl show failed")
		}
		return "", nil
	}
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "before uninstall") {
		t.Fatalf("want inspection error, got %v", err)
	}
	assertPresent(t, f.installer.paths.unit)
}

func TestServiceUninstallRejectsUnsafeStateFile(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	if err := os.Chmod(f.installer.paths.config, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.installer.Uninstall(context.Background(), UninstallOptions{Purge: true})
	if err == nil || !strings.Contains(err.Error(), "unsafe file") {
		t.Fatalf("want unsafe state file error, got %v", err)
	}
}

func TestServiceUninstallPurgesLeftoversFromEarlierRun(t *testing.T) {
	f := installedFixture(t)
	unitRunner(f, map[string]string{ServiceName: f.installer.paths.unit})
	if _, err := f.installer.Uninstall(context.Background(), UninstallOptions{}); err != nil {
		t.Fatal(err)
	}
	assertPresent(t, f.installer.paths.config)
	summary, err := f.installer.Uninstall(context.Background(), UninstallOptions{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	assertMissing(t, f.installer.paths.stateDir)
	if len(summary.Retained) != 0 {
		t.Fatalf("leftover purge should retain nothing: %+v", summary)
	}
}
