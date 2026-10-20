package agentinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdUnitEscapesPaths(t *testing.T) {
	binary := "/opt/beat %name/agent"
	config := "/etc/beat $NODE/quote\"\\\n\t\x7f.json"
	unit := string(unitFile(binary, config))
	for _, want := range []string{`"/opt/beat %%name/agent"`, `"/etc/beat $NODE/quote\"\\\x0a\x09\x7f.json"`,
		"ExecStart=:\"", "User=root\n", "UMask=0077\n", "Restart=on-failure\n", "WantedBy=multi-user.target\n"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestServiceInstallUsesExistingConfig(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, f.installer.paths.stateDir)
	if err := f.installer.saveJSON(f.installer.paths.config, f.result.Config); err != nil {
		t.Fatal(err)
	}
	if err := f.installer.Install(context.Background(), f.installer.paths.config); err != nil {
		t.Fatal(err)
	}
	if f.requests.Load() != 0 {
		t.Fatal("local service installation contacted the enrollment endpoint")
	}
	assertMode(t, f.installer.paths.unit, privateFile)
	if err := f.installer.Install(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestServiceInstallRejectsMissingOrInvalidConfig(t *testing.T) {
	f := newFixture(t)
	if err := f.installer.Install(context.Background(), ""); err == nil {
		t.Fatal("expected missing config error")
	}
	mustWrite(t, f.installer.paths.config, []byte(`{}`), privateFile)
	if err := f.installer.Install(context.Background(), ""); err == nil {
		t.Fatal("expected invalid config error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.installer.Install(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestServiceInstallRejectsDifferentConfigPath(t *testing.T) {
	f := newFixture(t)
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(f.installer.paths.stateDir, "other.json")
	if err := f.installer.saveJSON(other, f.result.Config); err != nil {
		t.Fatal(err)
	}
	if err := f.installer.Install(context.Background(), other); err == nil {
		t.Fatal("expected config conflict")
	}
}

func TestServiceInstallHandlesWriteAndCommandErrors(t *testing.T) {
	for _, failure := range []string{"binary", "unit", "source"} {
		t.Run(failure, func(t *testing.T) {
			f := newFixture(t)
			mustMkdir(t, f.installer.paths.stateDir)
			if err := f.installer.saveJSON(f.installer.paths.config, f.result.Config); err != nil {
				t.Fatal(err)
			}
			f.installer.write = func(path string, data []byte, mode os.FileMode) error {
				if failure == "binary" && path == f.installer.paths.binary || failure == "unit" && path == f.installer.paths.unit {
					return errors.New("write failed")
				}
				return atomicWrite(path, data, mode)
			}
			if failure == "source" {
				f.installer.executable = func() (string, error) { return "", errors.New("source failed") }
			}
			if err := f.installer.Install(context.Background(), ""); err == nil {
				t.Fatal("expected installation error")
			}
		})
	}
}

func TestRunSystemctlPropagatesArgumentsAndFailures(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fake systemctl")
	mustWrite(t, path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), executableFile)
	output, err := runSystemctl(context.Background(), path, []string{"--system", "show", "argument with spaces"})
	if err != nil || output != "--system\nshow\nargument with spaces\n" {
		t.Fatalf("unexpected command result: %q, %v", output, err)
	}
	mustWrite(t, path, []byte("#!/bin/sh\nexit 1\n"), executableFile)
	if _, err := runSystemctl(context.Background(), path, []string{"--system", "enable"}); err == nil {
		t.Fatal("expected failed systemctl")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runSystemctl(ctx, path, []string{"--system", "enable"}); err == nil {
		t.Fatal("expected command cancellation")
	}
}

func TestMissingUnitExitStatusIsAccepted(t *testing.T) {
	f := newFixture(t)
	f.installer.run = func(_ context.Context, _ string, args []string) (string, error) {
		if args[1] == "show" {
			return "LoadState=not-found\nFragmentPath=\nDropInPaths=\n", errors.New("unit not found")
		}
		return "", nil
	}
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSystemdProperties(t *testing.T) {
	path := "/etc/systemd/system/beat-agent.service"
	if err := validateSystemdUnit("LoadState=loaded\nFragmentPath="+path+"\nDropInPaths=\n", path); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{
		"LoadState=masked\nFragmentPath=\nDropInPaths=\n",
		"LoadState=loaded\nFragmentPath=/other/unit\nDropInPaths=\n",
		"LoadState=loaded\nFragmentPath=\nDropInPaths=\n",
		"LoadState=not-found\nFragmentPath=\nDropInPaths=/override\n",
		"LoadState=not-found\nOther=\nDropInPaths=\n",
	} {
		if err := validateSystemdUnit(output, path); err == nil {
			t.Fatalf("accepted systemd state %q", output)
		}
	}
}
