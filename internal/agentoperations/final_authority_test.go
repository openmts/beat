//go:build linux

package agentoperations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestConfigurationLinksAndForeignOwnershipAreRejected(t *testing.T) {
	directory := privateTestDir(t)
	path := filepath.Join(directory, "private")
	if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateConfigFile(link); err == nil {
		t.Fatal("symlinked configuration accepted")
	}
	hard := filepath.Join(directory, "hardlink")
	if err := os.Link(path, hard); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateConfigFile(hard); err == nil {
		t.Fatal("multiply linked configuration accepted")
	}
	if os.Geteuid() == 0 {
		foreign := filepath.Join(directory, "foreign")
		if err := os.WriteFile(foreign, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(foreign, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		if err := CheckPrivateConfigFile(foreign); err == nil {
			t.Fatal("another account's configuration accepted")
		}
	}
}

func TestRunnerInitializationRejectsObstructedOrForeignState(t *testing.T) {
	if _, err := NewRunner(RunnerConfig{Local: Config{Enabled: true}}); err == nil {
		t.Fatal("missing enabled configuration accepted")
	}
	path := filepath.Join(privateTestDir(t), "state")
	if err := os.WriteFile(path, []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{Enabled: true, StateDir: path}
	if _, err := NewRunner(RunnerConfig{Local: config, Gateway: &fakeGateway{}}); err == nil {
		t.Fatal("regular file used as state directory")
	}
	if os.Geteuid() == 0 {
		root := privateTestDir(t)
		if err := os.Chown(root, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		config.StateDir = filepath.Join(privateTestDir(t), "state")
		config.FileRoots = []FileRoot{{ID: "foreign", Path: root, Read: true, MaximumBytes: 1024}}
		if _, err := NewRunner(RunnerConfig{Local: config, Gateway: &fakeGateway{}}); err == nil {
			t.Fatal("foreign transfer root admitted")
		}
	}
}

func TestDefaultLinuxTaskActuallyDropsTheControllerIdentity(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("identity-drop fixture requires a privileged controller")
	}
	config := testConfig(t)
	config.Commands.RunAsUser = "nobody"
	config.Commands.AllowRoot = false
	if err := os.Chown(config.FileRoots[0].Path, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	lease := testLease("id -u; id -G")
	lease.Payload.Limits.RunAsRoot = false
	result, err := engine.Run(t.Context(), execution{lease: lease, directory: privateTestDir(t), running: func() error { return nil }})
	if err != nil || result.State != "succeeded" || string(result.Stdout) != "65534\n65534\n" {
		t.Fatalf("identity was not isolated: state=%s output=%q err=%v", result.State, result.Stdout, err)
	}
}

func TestCanceledExecutionAndUnavailableStateNeverLaunch(t *testing.T) {
	config := testConfig(t)
	settings, err := prepareSettings(config, "")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := newEngine(settings)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := engine.Run(ctx, execution{lease: testLease("touch forbidden"), directory: privateTestDir(t), running: func() error { t.Fatal("canceled job started"); return nil }})
	if !errors.Is(err, context.Canceled) || result.State != "interrupted" {
		t.Fatal("pre-canceled task ran")
	}
	runner, _ := testRunner(t, &fakeGateway{})
	original := runner.journals.root
	runner.journals.root = filepath.Join(original, "absent")
	if err := runner.Recover(t.Context()); !errors.Is(err, ErrStorage) {
		t.Fatal("missing state directory ignored")
	}
	runner.journals.root = original
	if err := runner.resume(t.Context(), journal{State: "unknown"}); !errors.Is(err, ErrStorage) {
		t.Fatal("unknown journal state accepted")
	}
	if err := runner.start(ctx, journal{Lease: protocol.Lease{}}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled authorization proceeded")
	}
}
