// v219：Agent 命令入口与可选运行时的失败关闭覆盖。
//
// 覆盖三类此前没有运行证据的分支：enroll 的参数/秘密来源校验、诊断运行时的装配失败、
// 以及可选运行时（更新/诊断）在启动失败或关闭时的收尾。
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/acceleratorcollect"
	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentdiagnostics"
	"github.com/beat/backend/internal/agentinstall"
	"github.com/beat/backend/internal/agentoperations"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/model"
)

func TestEnrollCommandRejectsInvalidInvocation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"positional", []string{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-stdin", "extra"}, "does not accept positional arguments"},
		{"config", []string{"beat-agent", "--config", "/private/agent.json", "enroll", "--server", "https://beat.example.com", "--grant-stdin"}, "--config is for foreground mode"},
		{"ssh-port", []string{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-stdin", "--ssh-port", "0"}, "ssh-port must be between 1 and 65535"},
		{"missing-grant", []string{"beat-agent", "enroll", "--server", "https://beat.example.com"}, "--grant-stdin or --grant-file is required"},
		{"conflicting-grant", []string{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-stdin", "--grant-file", "/private/grant.txt"}, "use either --grant-stdin or --grant-file"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var received agentinstall.GrantOptions
			command := quietCommand(enrollActions(&received))
			command.Reader = strings.NewReader("beat_enroll_v1_secret\n")
			err := command.Run(context.Background(), testCase.args)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("enroll error = %v, want %q", err, testCase.want)
			}
			if received.GrantSecret != "" {
				t.Fatalf("rejected invocation still reached the installer: %+v", received)
			}
		})
	}
}

func TestReadGrantSecretRejectsUnusableSources(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.txt")
	loose := filepath.Join(directory, "loose.txt")
	if err := os.WriteFile(loose, []byte("beat_enroll_v1_loose\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		reader io.Reader
		flag   string
		file   string
		want   string
	}{
		{name: "missing-file", flag: "--grant-file", file: missing, want: "read enrollment grant file"},
		{name: "loose-file", flag: "--grant-file", file: loose, want: "must be a regular file with 0600 permissions"},
		{name: "unreadable-stdin", flag: "--grant-stdin", reader: failedReader{}, want: "enrollment grant input"},
		{name: "oversized-stdin", flag: "--grant-stdin", reader: strings.NewReader(strings.Repeat("a", maxGrantSecretInput+1)), want: "enrollment grant input is too long"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var received agentinstall.GrantOptions
			command := quietCommand(enrollActions(&received))
			command.Reader = testCase.reader
			args := []string{"beat-agent", "enroll", "--server", "https://beat.example.com", testCase.flag}
			if testCase.file != "" {
				args = append(args, testCase.file)
			}
			err := command.Run(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("grant error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestUninstallSummarySurfacesRetainedWriteFailure(t *testing.T) {
	summary := agentinstall.UninstallSummary{Retained: []string{"/etc/beat-agent/agent.json"}}
	if err := writeUninstallSummary(failedWriter{}, summary); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("retained summary error = %v", err)
	}
}

func TestDiagnosticsRuntimeValidatesItsTransportAndTargets(t *testing.T) {
	enabled := &agentdiagnostics.Config{Enabled: true}
	if runner, err := newDiagnostics(agent.Config{Diagnostics: enabled}); runner != nil || !errors.Is(err, agentdiagnostics.ErrTransport) {
		t.Fatalf("diagnostics runner = %v %v", runner, err)
	}
	settings := diagnosticsSettings(t)
	runner, err := newDiagnostics(agent.Config{ServerURL: "http://127.0.0.1:1", AgentToken: "fixture-token", Diagnostics: &settings})
	if err != nil || runner == nil {
		t.Fatalf("diagnostics runner = %v %v", runner, err)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("close diagnostics runner: %v", err)
	}
}

func TestAgentServicesClosesUpdatesWhenOptionalRuntimeFails(t *testing.T) {
	config := agent.Config{ServerURL: "http://127.0.0.1:1", AgentToken: "fixture-token", NodeName: "fixture",
		AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second,
		Updates:      &agentupdate.Settings{Enabled: true, NodeID: "node"},
		Accelerators: &acceleratorcollect.Config{Enabled: true, SampleIntervalSeconds: 1}}
	if err := runAgentServices(context.Background(), config, filepath.Join(t.TempDir(), "agent.json")); err == nil {
		t.Fatal("invalid accelerator policy was admitted with update runtime enabled")
	}
}

func TestAgentServicesRunsAndClosesEnabledDiagnostics(t *testing.T) {
	settings := diagnosticsSettings(t)
	config := agent.Config{ServerURL: "http://127.0.0.1:1", AgentToken: "fixture-token", NodeName: "fixture",
		AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second, Diagnostics: &settings}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runAgentServices(ctx, config, filepath.Join(t.TempDir(), "agent.json")); err != nil {
		t.Fatalf("run agent services: %v", err)
	}
}

// diagnosticsSettings 复用与 agentdiagnostics 校验一致的合法策略（MTR + 单目标白名单）。
func diagnosticsSettings(t *testing.T) agentdiagnostics.Config {
	t.Helper()
	return agentdiagnostics.Config{Enabled: true, MTRPath: "/usr/bin/mtr", StateDir: filepath.Join(t.TempDir(), "diagnostics"),
		PollInterval: "5s", AllowedTargets: []string{"8.8.8.8"},
		Maxima: agentdiagnostics.Maxima{Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops,
			DurationSeconds: model.DefaultIperfDurationSeconds, ParallelStreams: model.MinimumDiagnosticStreams,
			BitrateMbps: model.DefaultDiagnosticBitrateMbps}, MaximumConcurrent: 1}
}

func TestEnrollCommandSurfacesInstallerFailure(t *testing.T) {
	actions := enrollActions(&agentinstall.GrantOptions{})
	failure := errors.New("enrollment rejected")
	actions.enroll = func(context.Context, agentinstall.GrantOptions) (agentinstall.Summary, error) {
		return agentinstall.Summary{}, failure
	}
	command := quietCommand(actions)
	command.Reader = strings.NewReader("beat_enroll_v1_secret\n")
	err := command.Run(context.Background(), []string{"beat-agent", "enroll", "--server", "https://beat.example.com", "--grant-stdin"})
	if !errors.Is(err, failure) {
		t.Fatalf("enroll error = %v, want %v", err, failure)
	}
}

// 上报地址不合法时必须在构造监控组件时就失败关闭，而不是带着空端点上路。
func TestMonitoringComponentsRejectInvalidServerURL(t *testing.T) {
	config := agent.Config{ServerURL: "beat.example.com", AgentToken: "fixture-token", NodeName: "fixture", AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second}
	if _, err := monitoringComponents(config); err == nil {
		t.Fatal("monitoring components accepted a server URL without a scheme")
	}
}

// 带查询串的上报地址会被运营客户端拒绝：可选的运营运行时此时必须让启动失败，而不是静默降级。
func TestAgentServicesRejectsOperationsClientWithQueryInServerURL(t *testing.T) {
	config := agent.Config{ServerURL: "https://beat.example.com?node=1", AgentToken: "fixture-token", NodeName: "fixture",
		AdvertisedHost: "127.0.0.1", SSHPort: 22, ReportInterval: time.Second,
		Operations: &agentoperations.Config{Enabled: true, StateDir: filepath.Join(t.TempDir(), "operations")}}
	err := runAgentServices(context.Background(), config, filepath.Join(t.TempDir(), "agent.json"))
	if err == nil {
		t.Fatal("operations client accepted a server URL with a query string")
	}
}
