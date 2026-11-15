package server

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	serverInstallPath = "/usr/local/bin/beat-server"
	serverBinaryDir   = "/usr/local/bin"
	serverConfigDir   = "/etc/beat"
	serverConfigPath  = "/etc/beat/server.yaml"
	serverDataDir     = "/var/lib/beat"
	serverServicePath = "/etc/systemd/system/beat-server.service"
	legacyServerEnv   = "/etc/beat/beat-server.env"
)

const serverDefaultConfig = `# Beat Server configuration. Command-line flags override these values.
listen: "[::]:9099"
data_dir: /var/lib/beat
agent_binary_dir: /usr/local/bin
demo_mode: false
enrollment_token_ttl: 10m

# Native HTTPS protects both the web console and gRPC. Keep the private key
# readable only by root.
# tls_cert: /etc/beat/tls/server.crt
# tls_key: /etc/beat/tls/server.key

# Optional fixed enrollment token, at least 32 characters. Prefer short-lived
# tokens generated from the admin console.
# agent_token: replace-with-at-least-32-random-characters

# SQLite path, MTS directory, retention, WAL policy and site identity are
# maintained in the Beat admin console settings page.
`

const serverSystemdUnit = `[Unit]
Description=Beat node telemetry server
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/local/bin/beat-server --config /etc/beat/server.yaml
ConfigurationDirectory=beat
ConfigurationDirectoryMode=0750
WorkingDirectory=/var/lib/beat
StateDirectory=beat
StateDirectoryMode=0750
Restart=on-failure
RestartSec=3
TimeoutStopSec=30
LimitNOFILE=65536
UMask=0077

[Install]
WantedBy=multi-user.target`

// Install initializes the fixed Server layout used by the systemd unit. It is
// intentionally limited to root and standard paths so installation cannot be
// used as an arbitrary root file-write primitive.
func Install(args []string) error {
	fs := flag.NewFlagSet("beat-server install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noStart := false
	fs.BoolVar(&noStart, "no-start", false, "install and enable the service without starting it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("Server systemd installation is only supported on Linux")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("installing the Server requires root")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("read Server executable path: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve Server executable path: %w", err)
	}
	return installServer(executable, !noStart)
}

func installServer(source string, start bool) error {
	for path, mode := range map[string]os.FileMode{
		serverConfigDir: 0o750,
		serverDataDir:   0o750,
		serverBinaryDir: 0o755,
	} {
		if err := os.MkdirAll(path, mode); err != nil {
			return fmt.Errorf("create %s: %w", path, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("set permissions on %s: %w", path, err)
		}
	}

	if err := installFile(source, serverInstallPath, 0o755); err != nil {
		return fmt.Errorf("install Server executable: %w", err)
	}
	if err := installBundledAgentBinaries(filepath.Dir(source), serverBinaryDir); err != nil {
		return err
	}
	if err := writeServerConfigIfMissing(serverConfigPath, serverDefaultConfig); err != nil {
		return err
	}
	if _, err := os.Stat(legacyServerEnv); err == nil {
		fmt.Fprintf(os.Stderr, "warning: legacy %s detected; settings are now loaded from %s\n", legacyServerEnv, serverConfigPath)
	}
	if err := writeServerUnit(serverServicePath, serverSystemdUnit); err != nil {
		return err
	}
	if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, output)
	}
	if output, err := exec.Command("systemctl", "enable", "beat-server").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable beat-server: %w: %s", err, output)
	}
	removeLegacyServerLayout()
	if start {
		if output, err := exec.Command("systemctl", "restart", "beat-server").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl restart beat-server: %w: %s", err, output)
		}
	}
	fmt.Println("Server installed: /usr/local/bin/beat-server")
	fmt.Println("Configuration: /etc/beat/server.yaml")
	fmt.Println("Data directory: /var/lib/beat")
	fmt.Println("Service: /etc/systemd/system/beat-server.service")
	return nil
}

func removeLegacyServerLayout() {
	for _, path := range []string{
		"/opt/beat/bin/beat-agent-linux-amd64",
		"/opt/beat/bin/beat-agent-linux-arm64",
		"/opt/beat/agent/beat-agent.previous",
		"/opt/beat/agent/beat-agent",
	} {
		_ = os.Remove(path)
	}
	for _, dir := range []string{"/opt/beat/bin", "/opt/beat/agent", "/opt/beat"} {
		_ = os.Remove(dir)
	}
}

func installBundledAgentBinaries(sourceDir, targetDir string) error {
	for _, arch := range []string{"amd64", "arm64"} {
		source := ""
		for _, name := range []string{"beat-agent-linux-" + arch, "beat-agent-" + arch} {
			candidate := filepath.Join(sourceDir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				source = candidate
				break
			}
		}
		if source == "" {
			fmt.Fprintf(os.Stderr, "warning: Agent binary for %s is missing; downloads will be unavailable\n", arch)
			continue
		}
		target := filepath.Join(targetDir, "beat-agent-linux-"+arch)
		if err := installFile(source, target, 0o755); err != nil {
			return fmt.Errorf("install Agent package for %s: %w", arch, err)
		}
	}
	return nil
}

func writeServerConfigIfMissing(path, contents string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("set Server configuration permissions: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create Server configuration: %w", err)
	}
	if _, err := io.WriteString(file, contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("write Server configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync Server configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close Server configuration: %w", err)
	}
	return nil
}

func installFile(source, target string, mode os.FileMode) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("inspect install source %s: %w", source, err)
	}
	if targetInfo, err := os.Stat(target); err == nil {
		if os.SameFile(sourceInfo, targetInfo) {
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("set permissions on installed file %s: %w", target, err)
			}
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect install target %s: %w", target, err)
	}

	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	file, err := os.CreateTemp(filepath.Dir(target), ".beat-install-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if _, err := io.Copy(file, input); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func writeServerUnit(path, unit string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".beat-server.service-*")
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
