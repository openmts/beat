package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func validConfigJSON() string {
	return `{
  "server_url": "http://127.0.0.1:8080",
  "agent_token": "agent-secret",
  "node_name": "node-one",
  "advertised_host": "10.0.0.10",
  "ssh_port": 22,
  "report_interval": "15s"
}`
}

func TestLoadConfig(t *testing.T) {
	config, err := LoadConfig(writeConfig(t, validConfigJSON(), 0o600))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.NodeName != "node-one" || config.ReportInterval != 15*time.Second || config.SSHPort != 22 {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    os.FileMode
		want    string
	}{
		{name: "insecure permissions", content: validConfigJSON(), mode: 0o644, want: "permissions"},
		{name: "invalid json", content: "{", mode: 0o600, want: "decode"},
		{name: "unknown field", content: strings.Replace(validConfigJSON(), "\n}", ",\n\"extra\": true\n}", 1), mode: 0o600, want: "unknown field"},
		{name: "missing token", content: strings.Replace(validConfigJSON(), "agent-secret", "", 1), mode: 0o600, want: "agent_token"},
		{name: "invalid url", content: strings.Replace(validConfigJSON(), "http://127.0.0.1:8080", "file:///tmp/beat", 1), mode: 0o600, want: "server_url"},
		{name: "invalid port", content: strings.Replace(validConfigJSON(), "\"ssh_port\": 22", "\"ssh_port\": 70000", 1), mode: 0o600, want: "ssh_port"},
		{name: "invalid interval", content: strings.Replace(validConfigJSON(), "15s", "100ms", 1), mode: 0o600, want: "report_interval"},
		{name: "missing name", content: strings.Replace(validConfigJSON(), "node-one", "", 1), mode: 0o600, want: "node_name"},
		{name: "missing host", content: strings.Replace(validConfigJSON(), "10.0.0.10", "", 1), mode: 0o600, want: "advertised_host"},
		{name: "zero port", content: strings.Replace(validConfigJSON(), "\"ssh_port\": 22", "\"ssh_port\": 0", 1), mode: 0o600, want: "ssh_port"},
		{name: "multiple values", content: validConfigJSON() + "\n{}", mode: 0o600, want: "multiple JSON values"},
		{name: "trailing invalid", content: validConfigJSON() + "\n{", mode: 0o600, want: "decode config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tt.content, tt.mode))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected open error")
	}
}

func writeSSHConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write sshd config: %v", err)
	}
	return path
}

func TestReadSSHPortFromFile(t *testing.T) {
	t.Run("explicit port", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "# comment\nPort 30022\nProtocol 2\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 30022 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("defaults to 22 when missing", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "PermitRootLogin yes\nProtocol 2\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 22 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("missing file defaults to 22", func(t *testing.T) {
		port, err := readSSHPortFromFile(filepath.Join(t.TempDir(), "missing"))
		if err != nil || port != 22 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("inline comment ignored", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "Port 2222 # the port\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 2222 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("commented port ignored", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "#Port 2222\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 22 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("include expanded", func(t *testing.T) {
		dir := t.TempDir()
		includePath := writeSSHConfig(t, dir, "conf.d/extra.conf", "Port 30022\n")
		_ = includePath
		path := writeSSHConfig(t, dir, "sshd_config", "Include "+dir+"/conf.d/*.conf\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 30022 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("invalid port errors", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "Port not-a-number\n")
		if _, err := readSSHPortFromFile(path); err == nil {
			t.Fatal("expected invalid port error")
		}
	})
}

func TestLoadConfigResolvesPortFromSystem(t *testing.T) {
	restore := readSSHPort
	t.Cleanup(func() { readSSHPort = restore })
	readSSHPort = func() (int, error) { return 30022, nil }

	content := strings.Replace(validConfigJSON(), "  \"ssh_port\": 22,\n", "", 1)
	config, err := LoadConfig(writeConfig(t, content, 0o600))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.SSHPort != 30022 {
		t.Fatalf("port = %d, want 30022", config.SSHPort)
	}
}

func TestParseSSHConfigPortErrors(t *testing.T) {
	dir := t.TempDir()
	// Include 匹配到目录导致 glob 无匹配但无错误——用非法模式测试
	path := writeSSHConfig(t, dir, "sshd_config", "Include [invalid\n")
	if _, err := readSSHPortFromFile(path); err == nil {
		t.Fatal("expected include pattern error")
	}
}

func TestReadSSHPortEdgeCases(t *testing.T) {
	t.Run("multiple port directives uses first", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "Port 2222\nPort 3333\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 2222 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("port directive case insensitive", func(t *testing.T) {
		dir := t.TempDir()
		path := writeSSHConfig(t, dir, "sshd_config", "PORT 30022\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 30022 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("explicit port 22 in include not skipped", func(t *testing.T) {
		dir := t.TempDir()
		writeSSHConfig(t, dir, "conf.d/extra.conf", "Port 22\n")
		path := writeSSHConfig(t, dir, "sshd_config", "Include "+dir+"/conf.d/*.conf\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 22 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("main port wins over include default", func(t *testing.T) {
		dir := t.TempDir()
		writeSSHConfig(t, dir, "conf.d/empty.conf", "PermitRootLogin yes\n")
		path := writeSSHConfig(t, dir, "sshd_config",
			"Include "+dir+"/conf.d/*.conf\nPort 30022\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 30022 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("first include port wins across multiple includes", func(t *testing.T) {
		dir := t.TempDir()
		writeSSHConfig(t, dir, "conf.d/a.conf", "Port 40022\n")
		writeSSHConfig(t, dir, "conf.d/b.conf", "Port 50022\n")
		path := writeSSHConfig(t, dir, "sshd_config", "Include "+dir+"/conf.d/*.conf\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 40022 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("include without port falls through to main port", func(t *testing.T) {
		dir := t.TempDir()
		writeSSHConfig(t, dir, "conf.d/empty.conf", "# no port here\n")
		path := writeSSHConfig(t, dir, "sshd_config", "Include "+dir+"/conf.d/*.conf\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 22 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})

	t.Run("nested include", func(t *testing.T) {
		dir := t.TempDir()
		writeSSHConfig(t, dir, "conf.d/inner.conf", "Port 60022\n")
		writeSSHConfig(t, dir, "conf.d/mid.conf", "Include "+dir+"/conf.d/inner.conf\n")
		path := writeSSHConfig(t, dir, "sshd_config", "Include "+dir+"/conf.d/mid.conf\n")
		port, err := readSSHPortFromFile(path)
		if err != nil || port != 60022 {
			t.Fatalf("port = %d, err = %v", port, err)
		}
	})
}

func TestResolveSSHPortMissingFile(t *testing.T) {
	port, err := resolveSSHPort(filepath.Join(t.TempDir(), "missing"))
	if err != nil || port != nil {
		t.Fatalf("port = %v, err = %v, want nil", port, err)
	}
}
