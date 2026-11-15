package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearAgentEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"BEAT_AGENT_CONFIG", "BEAT_SERVER_URL", "BEAT_AGENT_TOKEN", "BEAT_AGENT_INTERVAL",
		"BEAT_NODE_NAME", "BEAT_AGENT_LABELS", "BEAT_AGENT_STATE_FILE", "BEAT_DISK_PATH",
	} {
		t.Setenv(key, "")
	}
}

func TestParseOptionsYAMLAndFlagPrecedence(t *testing.T) {
	clearAgentEnvironment(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	body := "server: https://[::1]:9443\n" +
		"token: yaml-token\n" +
		"interval: 30s\n" +
		"name: edge node\n" +
		"labels:\n" +
		"  role: edge\n" +
		"  region: cn-east\n" +
		"state_file: /etc/beat/agent-id\n" +
		"disk_path: /data\n" +
		"insecure_skip_verify: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEAT_SERVER_URL", "http://environment")

	opts, err := ParseOptions([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ServerURL != "https://[::1]:9443" || opts.Token != "yaml-token" || opts.Interval != 30*time.Second {
		t.Fatalf("YAML options were not applied: %+v", opts)
	}
	if opts.Name != "edge node" || opts.LabelsRaw != "region=cn-east,role=edge" {
		t.Fatalf("unexpected node identity: %+v", opts)
	}
	if opts.StateFile != "/etc/beat/agent-id" || opts.DiskPath != "/data" || !opts.InsecureSkipVerify {
		t.Fatalf("unexpected YAML options: %+v", opts)
	}

	opts, err = ParseOptions([]string{
		"--config", path,
		"--server", "https://flags.example.com",
		"--interval", "45s",
		"--labels", "tenant=private",
		"--insecure-skip-verify=false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ServerURL != "https://flags.example.com" || opts.Interval != 45*time.Second ||
		opts.LabelsRaw != "tenant=private" || opts.InsecureSkipVerify {
		t.Fatalf("flags did not override YAML: %+v", opts)
	}
}

func TestParseOptionsTreatsEmptyYAMLAsDefaults(t *testing.T) {
	clearAgentEnvironment(t)
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(" # empty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseOptions([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ServerURL != "http://127.0.0.1:9099" || opts.Interval != 15*time.Second ||
		opts.LabelsRaw != "env=production" || opts.StateFile != "/etc/beat/agent-id" {
		t.Fatalf("empty YAML did not preserve defaults: %+v", opts)
	}
}

func TestParseOptionsRejectsInvalidYAML(t *testing.T) {
	clearAgentEnvironment(t)
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown":  "server: http://127.0.0.1:9099\nunknown_field: true\n",
		"interval": "interval: tomorrow\n",
		"zero":     "interval: 0s\n",
		"multi":    "server: http://127.0.0.1:9099\n---\nserver: http://127.0.0.1:9098\n",
		"labels":   "labels: [edge]\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ParseOptions([]string{"--config", path}); err == nil {
				t.Fatalf("invalid YAML %q was accepted", body)
			}
		})
	}
}

func TestParseOptionsUsesAgentConfigEnvironment(t *testing.T) {
	clearAgentEnvironment(t)
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("name: environment-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEAT_AGENT_CONFIG", path)
	opts, err := ParseOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Name != "environment-config" {
		t.Fatalf("BEAT_AGENT_CONFIG was not loaded: %+v", opts)
	}
}

func TestDefaultAgentConfigParses(t *testing.T) {
	clearAgentEnvironment(t)
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(agentDefaultConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseOptions([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ServerURL != "http://127.0.0.1:9099" || opts.Interval != 15*time.Second ||
		opts.LabelsRaw != "env=production" || opts.StateFile != "/etc/beat/agent-id" {
		t.Fatalf("unexpected default Agent configuration: %+v", opts)
	}
}
