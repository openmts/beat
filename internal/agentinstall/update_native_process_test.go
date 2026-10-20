//go:build linux

package agentinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentupdate"
)

type nativeUpdateControl struct{ Root, Unit, HelperUnit string }

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "--config" || os.Args[1] == "update-helper") {
		if err := nativeUpdateProcess(os.Args[1:]); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func nativeUpdateProcess(args []string) error {
	helper := args[0] == "update-helper"
	if helper {
		args = args[1:]
	}
	if len(args) != 2 || args[0] != "--config" {
		return errors.New("invalid fixture invocation")
	}
	configPath := args[1]
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "fixture.json"))
	if err != nil {
		return err
	}
	var control nativeUpdateControl
	if err = json.Unmarshal(data, &control); err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.Base(control.Unit), "beat-update-fixture-") || !strings.HasPrefix(filepath.Base(control.HelperUnit), "beat-watchdog-fixture-") {
		return errors.New("invalid fixture unit scope")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if helper {
		return runNativeSupervisor(ctx, control)
	}
	config, err := agent.LoadConfig(configPath)
	if err != nil {
		return err
	}
	if config.NodeName != "native-update-fixture" {
		return errors.New("invalid fixture identity")
	}
	marker := filepath.Join(control.Root, "started", fmt.Sprint(os.Getpid()))
	if err = os.WriteFile(marker, []byte("ready"), privateFile); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func nativeInstaller(control nativeUpdateControl) *Installer {
	installer := NewInstaller()
	installer.paths = installPaths{config: filepath.Join(control.Root, "etc", "agent.json"), stateDir: filepath.Join(control.Root, "etc"),
		binary: filepath.Join(control.Root, "opt", "beat-agent"), unit: control.Unit, helperUnit: control.HelperUnit, systemd: "/run/systemd/system"}
	installer.container = func() bool { return false }
	installer.run = func(ctx context.Context, executable string, args []string) (string, error) {
		bound := append([]string(nil), args...)
		for index, arg := range bound {
			if arg == ServiceName {
				bound[index] = filepath.Base(control.Unit)
			}
			if arg == UpdateServiceName {
				bound[index] = filepath.Base(control.HelperUnit)
			}
		}
		output, err := runSystemctl(ctx, executable, bound)
		if err != nil {
			return output, err
		}
		for _, unit := range []string{control.Unit, control.HelperUnit} {
			mask := unit + ".d/10-timeout-abort.conf"
			target, linkErr := os.Readlink(mask)
			if linkErr == nil && target == "/dev/null" {
				output = strings.ReplaceAll(output, "DropInPaths="+mask+"\n", "DropInPaths=\n")
			}
		}
		return output, nil
	}
	return installer
}

func runNativeSupervisor(ctx context.Context, control nativeUpdateControl) error {
	installer := nativeInstaller(control)
	supervisor := agentupdate.Supervisor{Interval: 100 * time.Millisecond, Build: func(ctx context.Context) (*agentupdate.Updater, error) {
		config, err := installer.UpdateConfiguration(ctx, installer.paths.config)
		if err != nil {
			return nil, err
		}
		if err = agentupdate.VerifyHelper(ctx, config); err != nil {
			return nil, err
		}
		config.StartupTimeout = 2 * time.Second
		config.ObservationWindow = 2 * time.Second
		config.MaximumObservationGap = time.Second
		config.Fetcher = nativeFileFetcher{path: filepath.Join(control.Root, "candidate.bin")}
		return agentupdate.New(config)
	}, OnError: func(err error) { _, _ = fmt.Fprintln(os.Stderr, "fixture watchdog:", err) }}
	return supervisor.Run(ctx)
}

type nativeFileFetcher struct{ path string }

func (nativeFileFetcher) TrustedServer() string { return "https://fixture.invalid" }

func (fetcher nativeFileFetcher) Fetch(_ context.Context, request agentupdate.FetchRequest) (agentupdate.FetchResponse, error) {
	file, err := os.Open(fetcher.path)
	if err != nil {
		return agentupdate.FetchResponse{}, err
	}
	reader := io.NewSectionReader(file, request.Offset, request.Length)
	return agentupdate.FetchResponse{Body: nativeSection{Reader: reader, file: file}, Offset: request.Offset, Length: request.Length, Total: request.Assignment.Artifact.Size}, nil
}

type nativeSection struct {
	io.Reader
	file *os.File
}

func (section nativeSection) Close() error { return section.file.Close() }

func nativeSystemctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "/usr/bin/systemctl", append([]string{"--system"}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(data)), err
}

func nativeWait(t *testing.T, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("native update fixture timed out")
		}
	}
}
