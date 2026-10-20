package agentinstall

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
)

type fixture struct {
	installer   *Installer
	options     JoinOptions
	result      agentjoin.Result
	requests    atomic.Int64
	calls       [][]string
	failCommand string
	respond     func(http.ResponseWriter, *http.Request)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	paths := installPaths{config: filepath.Join(root, "etc", "agent.json"), stateDir: filepath.Join(root, "etc"),
		binary: filepath.Join(root, "opt", "beat-agent"), unit: filepath.Join(root, "systemd", ServiceName),
		systemd: filepath.Join(root, "run")}
	mustMkdir(t, paths.systemd)
	source := filepath.Join(root, "source-agent")
	mustWrite(t, source, []byte("test agent executable"), executableFile)
	token, err := agentcredential.GenerateFrom(bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{installer: NewInstaller()}
	f.installer.paths, f.installer.goos = paths, "linux"
	f.installer.euid = func() int { return 0 }
	f.installer.hostname = func() (string, error) { return "test-node", nil }
	f.installer.sshPort = func() (int, error) { return 2222, nil }
	f.installer.executable = func() (string, error) { return source, nil }
	f.installer.lookPath = func(string) (string, error) { return "/usr/bin/systemctl", nil }
	f.installer.credential = func() (agentcredential.Token, error) { return token, nil }
	f.installer.run = f.run
	f.respond = f.serveJoin
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.respond(w, r)
	}))
	t.Cleanup(server.Close)
	f.options = JoinOptions{ServerURL: server.URL, Token: agentjoin.TokenPrefix + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), AllowHTTP: true}
	f.result = agentjoin.Result{NodeID: "node-id", Config: agentjoin.Config{
		ServerURL: server.URL, AgentToken: token.Plaintext, NodeName: "test-node",
		AdvertisedHost: "192.0.2.8", SSHPort: 2222, ReportInterval: "5s"}}
	return f
}

func (f *fixture) run(_ context.Context, _ string, args []string) (string, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if args[1] == f.failCommand {
		return "", errors.New("test systemctl failure")
	}
	if args[1] == "show" {
		return "FragmentPath=\nDropInPaths=\nLoadState=not-found\n", nil
	}
	return "", nil
}

func (f *fixture) serveJoin(w http.ResponseWriter, r *http.Request) {
	var request agentjoin.Request
	if r.URL.Path != "/api/v1/agents/join" || r.Method != http.MethodPost ||
		r.Header.Get("Authorization") != "Bearer "+f.options.Token {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := request.Validate(); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(agentjoin.Envelope[agentjoin.Result]{Code: http.StatusOK, Message: "success", Data: f.result})
}

func (f *fixture) assertInstalled(t *testing.T) {
	t.Helper()
	config, err := agent.LoadConfig(f.installer.paths.config)
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentToken != f.result.Config.AgentToken || config.SSHPort != 2222 {
		t.Fatalf("unexpected config: %+v", config)
	}
	for _, path := range []string{f.installer.paths.config, f.installer.paths.unit,
		filepath.Join(f.installer.paths.stateDir, "enrollment.json"), filepath.Join(f.installer.paths.stateDir, "installation.json")} {
		assertMode(t, path, privateFile)
	}
	assertMode(t, f.installer.paths.binary, executableFile)
	for _, call := range []string{"daemon-reload", "enable --now " + ServiceName, "is-active --quiet " + ServiceName} {
		found := false
		for _, args := range f.calls {
			if strings.Join(args[1:], " ") == call {
				found = true
			}
		}
		if !found {
			t.Errorf("missing systemctl %s", call)
		}
	}
	state, err := os.ReadFile(filepath.Join(f.installer.paths.stateDir, "enrollment.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), f.options.Token) {
		t.Fatal("join token persisted in plaintext")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, privateDirectory); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, privateFile); err != nil {
		t.Fatal(err)
	}
	// 权限反例不能依赖执行测试时的 umask。
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s: want mode %04o, got %04o", path, mode, info.Mode().Perm())
	}
}
