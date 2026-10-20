//go:build linux

package agentupdate_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/agentrollouthttp"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type transportFixture struct {
	schema       int
	db           *store.SQLiteStore
	metrics      *store.MTSStore
	nodes        *store.NodeStore
	node         *model.Node
	owner        agentrollout.Actor
	service      *agentrollout.Service
	rollout      agentrollout.Rollout
	server       *httptest.Server
	tls          *tls.Config
	clock        atomic.Int64
	key          ed25519.PrivateKey
	token        string
	old, next    []byte
	config       agentupdate.Config
	manager      *transportManager
	mu           sync.Mutex
	requests     []transportRequest
	rejectReport atomic.Bool
	observations chan struct{}
}

type transportRequest struct{ path, nonce, offset string }

type transportManager struct{ calls atomic.Int64 }

func (*transportManager) Kind() string { return "systemd" }

func (manager *transportManager) Restart(context.Context) error { manager.calls.Add(1); return nil }

func (f *transportFixture) now() time.Time { return time.Unix(0, f.clock.Load()).UTC() }

func (f *transportFixture) advance(delta time.Duration) { f.clock.Add(int64(delta)) }

// syncClock 把夹具时间轴推到不早于给定的真实时刻。节点上报的 ContactAt 由服务端真实时钟产生，
// 而 rollout 校验用夹具时钟；门禁满载时测试真实耗时可能超过 advance 推进的假时间，导致上报
// 时间戳「来自未来」而被 400 拒绝。这里保持两条时间轴不交叉，产品校验语义不变。
func (f *transportFixture) syncClock(at time.Time) {
	if at.IsZero() {
		return
	}
	for {
		current := f.clock.Load()
		if current >= at.UnixNano() || f.clock.CompareAndSwap(current, at.UnixNano()) {
			return
		}
	}
}

func transportMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newTransportFixture(t *testing.T) *transportFixture {
	t.Helper()
	f := &transportFixture{old: transportELF(256, 1), next: transportELF(int(agentupdate.ChunkBytes)+512, 2), manager: &transportManager{}, observations: make(chan struct{}, 8)}
	f.clock.Store(time.Now().UTC().UnixNano())
	f.key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{57}, ed25519.SeedSize))
	root := t.TempDir()
	var err error
	f.db, err = store.NewSQLiteStore(filepath.Join(root, "server.sqlite"))
	transportMust(t, err)
	t.Cleanup(func() { _ = f.db.Close() })
	transportMust(t, f.db.DB.QueryRowContext(t.Context(), "SELECT MAX(version) FROM schema_migrations").Scan(&f.schema))
	f.metrics, err = store.NewMTSStore(filepath.Join(root, "metrics"))
	transportMust(t, err)
	t.Cleanup(func() { _ = f.metrics.Close() })
	f.nodes = store.NewNodeStore(f.db.DB)
	f.seedIdentity(t)
	f.service, err = agentrollout.New(agentrollout.Config{Database: f.db.DB, Directory: filepath.Join(root, "release-cache"), Keys: []agentrollout.TrustedKey{{ID: "fixture-root", PublicKey: f.key.Public().(ed25519.PublicKey), NotBefore: f.now().Add(-time.Hour), ExpiresAt: f.now().Add(2 * time.Hour)}}, Sigstore: transportVerifier{}, Enabled: true, ServerSchema: f.schema, ProtocolVersion: model.AgentReportProtocol, Now: f.now})
	transportMust(t, err)
	f.serve(t)
	install := filepath.Join(root, "install")
	transportMust(t, os.Mkdir(install, 0o700))
	executable := filepath.Join(install, "beat-agent")
	transportMust(t, os.WriteFile(executable, f.old, 0o600))
	transportMust(t, os.Chmod(executable, 0o700))
	f.config = agentupdate.Config{Enabled: true, Executable: executable, NodeID: f.node.ID, ServerURL: f.server.URL, Keys: []protocol.SigningTrust{{KeyID: "fixture-root", PublicKey: f.key.Public().(ed25519.PublicKey), NotBefore: f.now().Add(-time.Hour), ExpiresAt: f.now().Add(2 * time.Hour)}}, Manager: f.manager, OS: "linux", Arch: runtime.GOARCH, AgentProtocol: model.AgentReportProtocol, Now: f.now, StartupTimeout: time.Minute, ObservationWindow: 2 * time.Second, MaximumObservationGap: time.Second}
	f.config.Installed, err = agentupdate.Bootstrap(t.Context(), agentupdate.BootstrapOptions{Config: f.config, Manifest: f.signed(t, f.old, "1.0.0"), ArtifactID: "native"})
	transportMust(t, err)
	return f
}

func (f *transportFixture) seedIdentity(t *testing.T) {
	t.Helper()
	credential, err := agentcredential.GenerateFrom(bytes.NewReader(bytes.Repeat([]byte{9}, 32)))
	transportMust(t, err)
	f.token = credential.Plaintext
	f.node, err = f.nodes.CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "tls-update-fixture", Host: "127.0.0.1", Port: 22}, store.AgentCredential{Hash: credential.Hash, Prefix: credential.Prefix, CreatedAt: f.now()})
	transportMust(t, err)
	_, err = f.db.DB.Exec("UPDATE nodes SET agent_version='1.0.0',os='linux',arch=?,last_seen=? WHERE id=?", runtime.GOARCH, f.now(), f.node.ID)
	transportMust(t, err)
	f.owner = agentrollout.Actor{UserID: "fixture-owner", SessionID: "fixture-session", RequestID: "fixture-request"}
	admins := store.NewAdminStore(f.db.DB)
	user := model.AdminUser{ID: f.owner.UserID, Username: "fixture-owner", DisplayName: "Fixture Owner", Role: model.AdminRoleOwner, Enabled: true, PasswordHash: "fixture-only", PasswordChangedAt: f.now(), CreatedAt: f.now(), UpdatedAt: f.now()}
	transportMust(t, admins.CreateUser(t.Context(), &user))
	until := f.now().Add(time.Hour)
	transportMust(t, admins.CreateSession(t.Context(), &model.AdminSession{ID: f.owner.SessionID, UserID: user.ID, TokenHash: bytes.Repeat([]byte{1}, 32), TokenPrefix: "fixture", CreatedAt: f.now(), LastActivityAt: f.now(), IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}))
}

func (f *transportFixture) serve(t *testing.T) {
	t.Helper()
	rolloutMux := http.NewServeMux()
	agentrollouthttp.New(f.service).Register(rolloutMux)
	protected := middleware.AgentAuth(f.nodes, "", nil)(rolloutMux)
	reports := handler.NewNodeHandler(f.nodes, f.metrics).WithMetricCatalog(store.NewMetricCatalogStore(f.db.DB))
	reportHandler := middleware.AgentReportAuth(f.nodes, "", nil)(http.HandlerFunc(reports.HandleNodeReport))
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer "+f.token {
			t.Error("request escaped fixture credential/path")
		}
		f.mu.Lock()
		f.requests = append(f.requests, transportRequest{r.URL.Path, r.Header.Get("X-Beat-Rollout-Nonce"), r.Header.Get("Range")})
		f.mu.Unlock()
		if r.URL.Path == "/api/v1/nodes/report" {
			if f.rejectReport.Load() {
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{"data":{"protocol_version":2,"contact_accepted":true,"metrics_accepted":false}}`))
				if err != nil {
					t.Error(err)
				}
				return
			}
			reportHandler.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/agents/rollout/observations" {
			// 观察请求被拒时把状态与响应体打进测试日志，门禁偶发失败才能直接定位原因。
			captured := &statusCapture{ResponseWriter: w, status: http.StatusOK}
			protected.ServeHTTP(captured, r)
			if captured.status != http.StatusOK {
				t.Logf("observation rejected: status=%d body=%s", captured.status, captured.body.String())
			}
			select {
			case f.observations <- struct{}{}:
			default:
			}
			return
		}
		protected.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(f.server.Certificate())
	f.tls = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	// HTTPReporter没有transport参数；只在本串行fixture里使用测试TLS根，清理时恢复。
	previous := http.DefaultTransport
	http.DefaultTransport = f.server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previous; f.server.Client().CloseIdleConnections() })
}

func (f *transportFixture) signed(t *testing.T, data []byte, version string) protocol.SignedManifest {
	t.Helper()
	digest := sha256.Sum256(data)
	manifest := protocol.Manifest{SchemaVersion: 1, Version: version, Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: f.now().Add(-time.Minute), ExpiresAt: f.now().Add(time.Hour), MinimumServerSchema: f.schema, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "fixture-root", Artifacts: []protocol.Artifact{{ID: "native", OS: "linux", Arch: runtime.GOARCH, Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), SBOMDigest: strings.Repeat("c", 64), ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}}}
	payload, err := protocol.CanonicalManifest(manifest)
	transportMust(t, err)
	return protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(f.key, payload), SigstoreBundle: []byte("private-fixture-bundle")}
}

func (f *transportFixture) publish(t *testing.T) {
	t.Helper()
	release, err := f.service.Import(t.Context(), f.owner, f.signed(t, f.next, "2.0.0"))
	transportMust(t, err)
	transportMust(t, f.service.PutArtifact(t.Context(), f.owner, agentrollout.ArtifactUpload{ReleaseID: release.ID, ArtifactID: "native", Body: bytes.NewReader(f.next)}))
	f.rollout, err = f.service.Create(t.Context(), f.owner, agentrollout.CreateInput{Name: "private TLS fixture", ReleaseID: release.ID, NodeIDs: []string{f.node.ID}, Policy: agentrollout.Policy{CanaryCount: 1, WaveSize: 1, ObservationSeconds: 30, MaxRestarts: 3, MaxProtocolErrors: 1}})
	transportMust(t, err)
	transportMust(t, f.service.Promote(t.Context(), f.owner, f.rollout.ID))
}

type statusCapture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *statusCapture) WriteHeader(code int) { c.status = code; c.ResponseWriter.WriteHeader(code) }

func (c *statusCapture) Write(data []byte) (int, error) {
	c.body.Write(data)
	return c.ResponseWriter.Write(data)
}

func transportELF(size int, marker byte) []byte {
	data := make([]byte, size)
	copy(data, "\x7fELF")
	data[4] = 2
	data[5] = 1
	data[6] = 1
	order := binary.LittleEndian
	order.PutUint16(data[16:], 2)
	machine := uint16(62)
	if runtime.GOARCH == "arm64" {
		machine = 183
	}
	order.PutUint16(data[18:], machine)
	order.PutUint32(data[20:], 1)
	order.PutUint64(data[24:], 0x400080)
	order.PutUint64(data[32:], 64)
	order.PutUint16(data[52:], 64)
	order.PutUint16(data[54:], 56)
	order.PutUint16(data[56:], 1)
	order.PutUint32(data[64:], 1)
	order.PutUint32(data[68:], 5)
	order.PutUint64(data[80:], 0x400000)
	order.PutUint64(data[96:], uint64(size))
	order.PutUint64(data[104:], uint64(size))
	data[size-1] = marker
	return data
}
