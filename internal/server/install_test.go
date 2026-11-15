package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"beat/internal/config"
)

func TestServerInstallTemplatesMatchDeployFiles(t *testing.T) {
	configBody, err := os.ReadFile("../../deploy/server.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(configBody)) != strings.TrimSpace(serverDefaultConfig) {
		t.Fatal("deploy/server.yaml and the generated Server configuration drifted")
	}

	unitBody, err := os.ReadFile("../../deploy/beat-server.service")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(unitBody)) != strings.TrimSpace(serverSystemdUnit) {
		t.Fatal("deploy/beat-server.service and the generated Server unit drifted")
	}
}

func TestServerDefaultConfigLoads(t *testing.T) {
	for _, key := range []string{"BEAT_CONFIG", "BEAT_LISTEN", "BEAT_DATA_DIR", "BEAT_AGENT_BINARY_DIR", "BEAT_DEMO_MODE", "BEAT_ENROLLMENT_TOKEN_TTL"} {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte(serverDefaultConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadServer([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "[::]:9099" || cfg.DataDir != "/var/lib/beat" || cfg.AgentBinaryDir != "/usr/local/bin" || cfg.DemoMode {
		t.Fatalf("unexpected default Server configuration: %+v", cfg)
	}
}

func TestWriteServerConfigIfMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := writeServerConfigIfMissing(path, serverDefaultConfig); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != serverDefaultConfig {
		t.Fatal("created Server configuration differs from template")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Server configuration mode = %o", info.Mode().Perm())
	}

	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:9098\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeServerConfigIfMissing(path, serverDefaultConfig); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "listen: 127.0.0.1:9098\n" {
		t.Fatal("existing Server configuration was overwritten")
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("existing Server configuration mode = %o", info.Mode().Perm())
	}
}

func TestInstallFileSkipsSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "beat-server")
	if err := os.WriteFile(path, []byte("server"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer before.Close()
	beforeInfo, err := before.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := installFile(path, path, 0o755); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeInfo, afterInfo) {
		t.Fatal("installing an already-installed file replaced the inode")
	}
	if afterInfo.Mode().Perm() != 0o755 {
		t.Fatalf("already-installed file mode = %o", afterInfo.Mode().Perm())
	}
}

func TestInstallBundledAgentBinaries(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()
	for _, arch := range []string{"amd64", "arm64"} {
		if err := os.WriteFile(filepath.Join(sourceDir, "beat-agent-linux-"+arch), []byte("agent-"+arch), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := installBundledAgentBinaries(sourceDir, targetDir); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		path := filepath.Join(targetDir, "beat-agent-linux-"+arch)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "agent-"+arch {
			t.Fatalf("installed %s body = %q", arch, body)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("installed %s mode = %o", arch, info.Mode().Perm())
		}
	}
}

func TestServerInstallScriptUsesBinaryCommand(t *testing.T) {
	body, err := os.ReadFile("../../deploy/install-server.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{"beat-server\" install", "beat-server\" install --no-start"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install script missing %q", want)
		}
	}
}
