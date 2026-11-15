package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInstallHelpers(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "download")
	if err := os.WriteFile(source, []byte("agent-binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "opt", "beat-agent")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installExecutable(source, target); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "agent-binary" {
		t.Fatalf("installed body = %q", body)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("installed mode = %o", info.Mode().Perm())
	}

	unit := filepath.Join(dir, "systemd", "beat-agent.service")
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSystemdUnit(unit, agentSystemdUnit); err != nil {
		t.Fatal(err)
	}
	unitBody, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(unitBody)) != agentSystemdUnit {
		t.Fatal("written systemd unit differs from template")
	}
	info, err = os.Stat(unit)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("systemd unit mode = %o", info.Mode().Perm())
	}
}

func TestAgentSystemdUnitMatchesDeployTemplate(t *testing.T) {
	body, err := os.ReadFile("../../deploy/beat-agent.service")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != agentSystemdUnit {
		t.Fatal("deploy/beat-agent.service and generated Agent unit drifted")
	}
	for _, want := range []string{
		"User=root",
		"Group=root",
		"ExecStart=/usr/local/bin/beat-agent --config /etc/beat/agent.yaml",
		"Restart=on-failure",
	} {
		if !strings.Contains(agentSystemdUnit, want) {
			t.Fatalf("Agent unit missing %q", want)
		}
	}
	if strings.Contains(agentSystemdUnit, "EnvironmentFile=") {
		t.Fatal("Agent unit must not load an environment file")
	}
	for _, unwanted := range []string{"ProtectSystem=strict", "ProtectHome=true", "NoNewPrivileges=true", "ReadWritePaths="} {
		if strings.Contains(agentSystemdUnit, unwanted) {
			t.Fatalf("Agent unit must not contain %q", unwanted)
		}
	}
}

func TestWriteAgentConfigIfMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := writeAgentConfigIfMissing(path, agentDefaultConfig); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != agentDefaultConfig {
		t.Fatal("created Agent configuration differs from template")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Agent configuration mode = %o", info.Mode().Perm())
	}

	if err := os.WriteFile(path, []byte("server: https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentConfigIfMissing(path, agentDefaultConfig); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "server: https://example.com\n" {
		t.Fatal("existing Agent configuration was overwritten")
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("existing Agent configuration mode = %o", info.Mode().Perm())
	}
}

func TestAgentConfigTemplateMatchesDeploySample(t *testing.T) {
	body, err := os.ReadFile("../../deploy/agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != agentDefaultConfig {
		t.Fatal("deploy/agent.yaml and the generated Agent configuration drifted")
	}
}
