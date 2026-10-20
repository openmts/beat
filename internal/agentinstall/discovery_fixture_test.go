package agentinstall

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentenrollment"
	"github.com/beat/backend/internal/api"
	"github.com/beat/backend/internal/store"
)

// discoveryOrigin 是测试里 Agent 使用的服务端地址；请求不会真正出网。
const discoveryOrigin = "https://beat.example.com"

// discoveryClientIP 是测试机器的来源地址，必须落在授权 CIDR 内。
const discoveryClientIP = "192.0.2.10:4000"

// routerTransport 把 Agent 的 HTTP 请求直接交给同进程的服务端路由，避免监听端口。
type routerTransport struct{ handler http.Handler }

func (transport routerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// 服务端要求入网流量走 TLS；测试在进程内模拟 https 部署，避免监听真实端口。
	request.RemoteAddr = discoveryClientIP
	request.TLS = &tls.ConnectionState{Version: tls.VersionTLS12}
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

type discoveryFixture struct {
	installer  *Installer
	enrollment *store.AgentEnrollmentStore
	groupID    string
	secret     agentenrollment.Secret
}

func newDiscoveryInstaller(t *testing.T, mode agentenrollment.ApprovalMode) *discoveryFixture {
	t.Helper()
	root := t.TempDir()
	paths := installPaths{config: filepath.Join(root, "etc", "agent.json"), stateDir: filepath.Join(root, "etc"),
		binary: filepath.Join(root, "opt", "beat-agent"), unit: filepath.Join(root, "systemd", ServiceName),
		systemd: filepath.Join(root, "run")}
	mustMkdir(t, paths.systemd)
	source := filepath.Join(root, "source-agent")
	mustWrite(t, source, []byte("test agent executable"), executableFile)
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	group, err := store.NewGroupStore(sqlite.DB).CreateGroup(context.Background(), "Default")
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	enrollment := store.NewAgentEnrollmentStore(sqlite.DB)
	router := api.NewRouter(store.NewNodeStore(sqlite.DB), store.NewGroupStore(sqlite.DB),
		store.NewSSHKeyStore(sqlite.DB), store.NewAlertRuleStore(sqlite.DB),
		store.NewAlertChannelStore(sqlite.DB), store.NewAlertEventStore(sqlite.DB), nil,
		api.WithAgentEnrollmentStore(enrollment))
	secret, err := agentenrollment.GenerateGrantSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	policy := agentenrollment.GrantPolicy{Name: "lab", Mode: mode, MaximumEnrollments: 4,
		DefaultGroupID: group.ID, NodeNamePrefix: "lab", Tags: []string{}, SSHPort: 22,
		UseSourceIPAsHost: true, SourceCIDRs: []string{"192.0.2.0/24"}, HostnamePattern: "^lab-[a-z0-9-]+$",
		ClaimRecovery: true, Enabled: true, ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := enrollment.CreateGrant(context.Background(),
		store.EnrollmentGrantInput{Policy: policy, Secret: secret, ActorID: "owner"}, time.Now()); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	installer := NewInstaller()
	installer.paths, installer.goos = paths, "linux"
	installer.euid = func() int { return 0 }
	installer.hostname = func() (string, error) { return "lab-1", nil }
	installer.sshPort = func() (int, error) { return 22, nil }
	installer.executable = func() (string, error) { return source, nil }
	installer.lookPath = func(string) (string, error) { return "/usr/bin/systemctl", nil }
	installer.client = &http.Client{Transport: routerTransport{handler: router.ServeHandler()}}
	installer.run = discoverySystemctl
	return &discoveryFixture{installer: installer, enrollment: enrollment, groupID: group.ID, secret: secret}
}

func discoverySystemctl(_ context.Context, _ string, args []string) (string, error) {
	if args[1] == "show" {
		return "FragmentPath=\nDropInPaths=\nLoadState=not-found\n", nil
	}
	return "", nil
}

func (fixture *discoveryFixture) options() GrantOptions {
	return GrantOptions{ServerURL: discoveryOrigin, GrantSecret: fixture.secret.Plaintext,
		PollInterval: 5 * time.Millisecond, PollTimeout: 3 * time.Second}
}
