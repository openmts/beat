package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	agentInstallPath    = "/usr/local/bin/beat-agent"
	agentConfigDir      = "/etc/beat"
	agentConfigPath     = "/etc/beat/agent.yaml"
	legacyAgentEnvPath  = "/etc/beat/beat-agent.env"
	agentStatePath      = "/etc/beat/agent-id"
	agentServicePath    = "/etc/systemd/system/beat-agent.service"
	legacyAgentPath     = "/opt/beat/agent/beat-agent"
	legacyAgentPrevious = "/opt/beat/agent/beat-agent.previous"
)

const agentDefaultConfig = `# Beat Agent configuration. Command-line flags override these values.
server: "http://127.0.0.1:9099"
token: ""
interval: 15s
name: ""
labels:
  env: production
state_file: /etc/beat/agent-id
disk_path: /
insecure_skip_verify: false
`

const agentSystemdUnit = `[Unit]
Description=Beat node telemetry agent
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/local/bin/beat-agent --config /etc/beat/agent.yaml
Restart=on-failure
RestartSec=3
TimeoutStopSec=20
LimitNOFILE=65536
UMask=0077

[Install]
WantedBy=multi-user.target`

// Install initializes the standard root-owned Agent layout. It is intentionally
// limited to fixed paths so an administrator cannot turn an explicit install
// command into an arbitrary root file-write primitive.
func Install() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Agent systemd installation is only supported on Linux")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing the Agent requires root")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("read Agent executable path: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve Agent executable path: %w", err)
	}
	return installAgent(executable)
}

func installAgent(source string) error {
	for path, mode := range map[string]os.FileMode{
		agentConfigDir:                 0o750,
		filepath.Dir(agentInstallPath): 0o755,
	} {
		if err := os.MkdirAll(path, mode); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("set permissions on %s: %w", path, err)
		}
	}

	_ = exec.Command("systemctl", "stop", "beat-agent").Run()
	if source != agentInstallPath {
		if err := installExecutable(source, agentInstallPath); err != nil {
			return err
		}
	} else if err := os.Chmod(agentInstallPath, 0o755); err != nil {
		return fmt.Errorf("set Agent permissions: %w", err)
	}
	if err := writeAgentConfigIfMissing(agentConfigPath, agentDefaultConfig); err != nil {
		return err
	}
	if _, err := os.Stat(legacyAgentEnvPath); err == nil {
		fmt.Fprintf(os.Stderr, "warning: legacy %s detected; settings are now loaded from %s\n", legacyAgentEnvPath, agentConfigPath)
	}
	if err := writeSystemdUnit(agentServicePath, agentSystemdUnit); err != nil {
		return err
	}
	if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, output)
	}
	removeLegacyAgentInstall()
	return nil
}

func removeLegacyAgentInstall() {
	for _, path := range []string{legacyAgentPrevious, legacyAgentPath} {
		_ = os.Remove(path)
	}
	for _, dir := range []string{"/opt/beat/agent", "/opt/beat/bin", "/opt/beat"} {
		_ = os.Remove(dir)
	}
}

func writeAgentConfigIfMissing(path, contents string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("set Agent configuration permissions: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create Agent configuration: %w", err)
	}
	if _, err := io.WriteString(file, contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write Agent configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync Agent configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Agent configuration: %w", err)
	}
	return nil
}

func installExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open Agent executable: %w", err)
	}
	defer input.Close()
	file, err := os.CreateTemp(filepath.Dir(target), ".beat-agent-install-*")
	if err != nil {
		return fmt.Errorf("create Agent installation file: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := io.Copy(file, input); err != nil {
		_ = file.Close()
		return fmt.Errorf("copy Agent executable: %w", err)
	}
	if err := file.Chmod(0o755); err != nil {
		_ = file.Close()
		return fmt.Errorf("set Agent executable permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync Agent executable: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Agent executable: %w", err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("install Agent executable: %w", err)
	}
	return nil
}

func writeSystemdUnit(path, unit string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".beat-agent.service-*")
	if err != nil {
		return fmt.Errorf("create systemd unit: %w", err)
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if _, err := io.WriteString(file, unit+"\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write systemd unit: %w", err)
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		return fmt.Errorf("set systemd unit permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync systemd unit: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close systemd unit: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("install systemd unit: %w", err)
	}
	return nil
}
