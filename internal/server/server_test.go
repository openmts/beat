package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"beat/internal/domain"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

func TestParseRange(t *testing.T) {
	now := time.Now()
	tests := []struct {
		input string
		span  time.Duration
		step  time.Duration
	}{
		{"1h", time.Hour, time.Minute},
		{"6h", 6 * time.Hour, 5 * time.Minute},
		{"24h", 24 * time.Hour, 15 * time.Minute},
		{"7d", 7 * 24 * time.Hour, time.Hour},
		{"invalid", 24 * time.Hour, 15 * time.Minute},
	}
	for _, test := range tests {
		span, step := parseRange(test.input, now)
		if span != test.span || step != test.step {
			t.Fatalf("parseRange(%q) = (%s, %s), want (%s, %s)", test.input, span, step, test.span, test.step)
		}
	}
}

func TestEnrollmentTokenIsRandom(t *testing.T) {
	first, err := newEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("newEnrollmentToken returned duplicate tokens")
	}
	if !strings.HasPrefix(first, "beat_enroll_") || len(first) < 40 {
		t.Fatalf("unexpected token format: %q", first)
	}
}

func TestBuildAgentAccessCommand(t *testing.T) {
	command := buildAgentAccessCommand("http://[::1]:9099", "beat_enroll_secret", "edge node", `region=cn-east,role=edge"quoted`)
	for _, want := range []string{
		"sudo tee /etc/beat/agent.yaml",
		"sudo install -m 0600 /dev/null /etc/beat/agent.yaml",
		"sudo chmod 0600 /etc/beat/agent.yaml",
		"sudo rm -f /etc/beat/beat-agent.env",
		"sudo systemctl enable beat-agent",
		"sudo systemctl restart beat-agent",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("access command missing %q: %s", want, command)
		}
	}
	for _, unwanted := range []string{"/downloads/beat-agent", "sudo /usr/local/bin/beat-agent", "--token ", "BEAT_AGENT_", "EnvironmentFile="} {
		if strings.Contains(command, unwanted) {
			t.Fatalf("access command must not contain %q: %s", unwanted, command)
		}
	}
	const marker = "sudo tee /etc/beat/agent.yaml >/dev/null <<'EOF'\n"
	start := strings.Index(command, marker)
	if start < 0 {
		t.Fatalf("access command has no YAML heredoc: %s", command)
	}
	start += len(marker)
	end := strings.Index(command[start:], "\nEOF\n")
	if end < 0 {
		t.Fatalf("access command has no YAML heredoc terminator: %s", command)
	}
	var configuration struct {
		ServerURL string            `yaml:"server"`
		Token     string            `yaml:"token"`
		Interval  string            `yaml:"interval"`
		Name      string            `yaml:"name"`
		Labels    map[string]string `yaml:"labels"`
		StateFile string            `yaml:"state_file"`
		DiskPath  string            `yaml:"disk_path"`
	}
	if err := yaml.Unmarshal([]byte(command[start:start+end]), &configuration); err != nil {
		t.Fatalf("access command contains invalid YAML: %v\n%s", err, command)
	}
	if configuration.ServerURL != "http://[::1]:9099" || configuration.Token != "beat_enroll_secret" ||
		configuration.Interval != "15s" || configuration.Name != "edge node" ||
		configuration.StateFile != "/etc/beat/agent-id" || configuration.DiskPath != "/" {
		t.Fatalf("unexpected Agent YAML configuration: %+v", configuration)
	}
	if configuration.Labels["region"] != "cn-east" || configuration.Labels["role"] != `edge"quoted` {
		t.Fatalf("unexpected Agent labels: %+v", configuration.Labels)
	}
	assertShellSyntax(t, command)
}

func TestBuildAgentDownloadCommandIncludesArchitecture(t *testing.T) {
	downloadURL := "http://[::1]:9099/downloads/beat-agent/arm64"
	checksum := strings.Repeat("a", 64)
	command := buildAgentDownloadCommand(downloadURL, "arm64", checksum)
	for _, want := range []string{
		"'http://[::1]:9099/downloads/beat-agent/arm64'",
		`[ "$host_arch" = 'arm64' ]`,
		"mktemp /tmp/beat-agent.XXXXXX",
		"printf '%s  %s\\n' '" + checksum + "' \"$tmp\" | sha256sum -c -",
		`chmod 0755 "$tmp"`,
		`sudo "$tmp" install`,
		`/usr/local/bin/beat-agent --version`,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("download command missing %q: %s", want, command)
		}
	}
	for _, unwanted := range []string{"useradd", "User=beat-agent", "ProtectSystem=strict", "NoNewPrivileges=true"} {
		if strings.Contains(command, unwanted) {
			t.Fatalf("download command must not contain %q: %s", unwanted, command)
		}
	}
	assertShellSyntax(t, command)
}

func assertShellSyntax(t *testing.T, command string) {
	t.Helper()
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(command)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shell syntax error: %v: %s\n%s", err, output, command)
	}
}

func TestSystemdUnitsRunAsRoot(t *testing.T) {
	for _, path := range []string{"../../deploy/beat-server.service", "../../deploy/beat-agent.service"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		unit := string(body)
		for _, want := range []string{"User=root", "Group=root", "Restart=on-failure"} {
			if !strings.Contains(unit, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
		for _, unwanted := range []string{"ProtectSystem=strict", "ProtectHome=true", "NoNewPrivileges=true", "ReadWritePaths="} {
			if strings.Contains(unit, unwanted) {
				t.Fatalf("%s must not contain %q", path, unwanted)
			}
		}
	}
}

func TestAgentDownloadByNameAndArchitecture(t *testing.T) {
	dir := t.TempDir()
	amd64Path := filepath.Join(dir, "beat-agent-linux-amd64")
	arm64Path := filepath.Join(dir, "beat-agent-linux-arm64")
	if err := os.WriteFile(amd64Path, []byte("amd64-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(arm64Path, []byte("arm64-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{agentBinaryDir: dir}
	router := chi.NewRouter()
	router.Get("/downloads/beat-agent/{arch}", s.handleAgentDownload)

	for arch, wantBody := range map[string]string{"amd64": "amd64-binary", "arm64": "arm64-binary"} {
		t.Run(arch, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/downloads/beat-agent/"+arch, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="beat-agent"` {
				t.Fatalf("Content-Disposition = %q", got)
			}
			if got := response.Body.String(); got != wantBody {
				t.Fatalf("body = %q, want %q", got, wantBody)
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/downloads/beat-agent/386", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unsupported architecture status = %d, want 404", response.Code)
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("node's edge"), `'node'\''s edge'`; got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func TestValidateNodeLabels(t *testing.T) {
	got, err := validateNodeLabels(map[string]string{" region ": " cn-east ", "role": "edge"})
	if err != nil {
		t.Fatal(err)
	}
	if got["region"] != "cn-east" || got["role"] != "edge" {
		t.Fatalf("validateNodeLabels() = %#v", got)
	}
	if _, err := validateNodeLabels(map[string]string{"": "value"}); err == nil {
		t.Fatal("validateNodeLabels() accepted an empty key")
	}
}

func TestDownsampleNode(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []domain.Metrics{
		{Timestamp: start, CPUUsage: 10, MemoryUsage: 20},
		{Timestamp: start.Add(10 * time.Second), CPUUsage: 20, MemoryUsage: 30},
		{Timestamp: start.Add(2 * time.Minute), CPUUsage: 40, MemoryUsage: 50},
	}
	got := downsampleNode(points, time.Minute)
	if len(got) != 2 {
		t.Fatalf("downsampleNode() returned %d points, want 2", len(got))
	}
	if got[0].CPUUsage != 15 || got[0].MemoryUsage != 25 {
		t.Fatalf("first bucket = %+v, want averaged 15/25", got[0])
	}
	if got[1].CPUUsage != 40 || got[1].MemoryUsage != 50 {
		t.Fatalf("second bucket = %+v, want 40/50", got[1])
	}
}

func TestSummarize(t *testing.T) {
	nodes := []domain.Node{
		{Status: domain.NodeOnline, Metrics: domain.Metrics{CPUUsage: 20, MemoryUsage: 40, DiskUsage: 60, NetworkRxBps: 100, NetworkTxBps: 50}},
		{Status: domain.NodeWarning, Metrics: domain.Metrics{CPUUsage: 40, MemoryUsage: 60, DiskUsage: 80, NetworkRxBps: 200, NetworkTxBps: 100}},
		{Status: domain.NodeOffline, Metrics: domain.Metrics{CPUUsage: 100, MemoryUsage: 100, DiskUsage: 100}},
	}
	got := summarize(nodes)
	want := OverviewSummary{NodeCount: 3, OnlineCount: 1, WarningCount: 1, OfflineCount: 1, AvgCPU: 30, AvgMemory: 50, AvgDisk: 70, NetworkRx: 300, NetworkTx: 150}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summarize() = %+v, want %+v", got, want)
	}
}
