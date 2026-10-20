package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/releasesignature"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

const rolloutOwnerPassword = "correct horse battery staple"

const rolloutTestOrigin = "https://beat.example.test"

type rolloutRouteFixture struct {
	router   *Router
	db       *store.SQLiteStore
	service  *agentrollout.Service
	owner    adminauth.LoginResult
	now      time.Time
	artifact []byte
	signed   protocol.SignedManifest
	verifier *rolloutRouteVerifier
}

type rolloutRouteVerifier struct {
	fail       bool
	panicValue string
}

func (v *rolloutRouteVerifier) Verify(_ context.Context, e releasesignature.Evidence) error {
	if v.panicValue != "" {
		panic(v.panicValue)
	}
	if v.fail || string(e.Bundle) != "fixture-bundle" {
		return agentrollout.ErrSignature
	}
	return nil
}

func (v *rolloutRouteVerifier) Policy() releasesignature.PolicyMetadata {
	value := releasesignature.PolicyMetadata{Version: 1, Root: []byte(`{"fixture":"offline-root"}`), VerifierSHA256: strings.Repeat("e", 64), ReleaseWorkflow: "https://github.com/example/beat/.github/workflows/release.yml", OIDCIssuer: "https://token.actions.githubusercontent.com"}
	digest := sha256.Sum256(value.Root)
	value.RootSHA256 = hex.EncodeToString(digest[:])
	revision := sha256.Sum256([]byte(strings.Join([]string{"1", value.RootSHA256, value.VerifierSHA256, value.CertificateIdentity, value.ReleaseWorkflow, value.OIDCIssuer}, "\x00")))
	value.Revision = hex.EncodeToString(revision[:])
	return value
}

func newRolloutRouteFixture(t *testing.T, enabled bool) *rolloutRouteFixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	secrets := testutil.NewSecrets(t, database.DB)
	security, err := adminauth.NewService(adminauth.ServiceConfig{Store: store.NewAdminStore(database.DB), Secrets: secrets, LegacyToken: "bootstrap-admin", Now: func() time.Time { return now }, Passwords: adminauth.NewPasswordHasher(adminauth.PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := security.Bootstrap(t.Context(), adminauth.BootstrapRequest{BootstrapToken: "bootstrap-admin", Username: "owner", DisplayName: "Owner", Password: rolloutOwnerPassword})
	if err != nil {
		t.Fatal(err)
	}
	if err := security.Reauthenticate(t.Context(), &owner.Principal, rolloutOwnerPassword, ""); err != nil {
		t.Fatal(err)
	}
	f := &rolloutRouteFixture{db: database, owner: owner, now: now, artifact: []byte("verified rollout fixture artifact"), verifier: &rolloutRouteVerifier{}}
	f.initializeService(t, enabled)
	f.router = NewRouter(store.NewNodeStore(database.DB), store.NewGroupStore(database.DB), store.NewSSHKeyStore(database.DB), store.NewAlertRuleStore(database.DB), store.NewAlertChannelStore(database.DB), store.NewAlertEventStore(database.DB), nil, WithAdminSecurity(security), WithAdminToken("bootstrap-admin"), WithAgentAuthentication("legacy-agent"), WithAgentRollouts(f.service))
	return f
}

func (f *rolloutRouteFixture) initializeService(t *testing.T, enabled bool) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = agentrollout.New(agentrollout.Config{Database: f.db.DB, Enabled: enabled, Directory: filepath.Join(t.TempDir(), "releases"), ServerSchema: 14, Keys: []agentrollout.TrustedKey{{ID: "root", PublicKey: public, NotBefore: f.now.Add(-time.Hour), ExpiresAt: f.now.Add(24 * time.Hour)}}, Sigstore: f.verifier, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(f.artifact)
	manifest := protocol.Manifest{SchemaVersion: 1, Version: "2.0.0", Commit: strings.Repeat("a", 40), Channel: "stable", BuiltAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(time.Hour), MinimumServerSchema: 14, MinimumProtocol: 1, MaximumProtocol: 2, ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "root", Artifacts: []protocol.Artifact{{ID: "linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary", Filename: "beat-agent", Entrypoint: "beat-agent", Size: int64(len(f.artifact)), SHA256: hex.EncodeToString(digest[:]), SBOMDigest: strings.Repeat("c", 64), ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}}}
	payload, err := agentrollout.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.signed = protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(private, payload), SigstoreBundle: []byte("fixture-bundle")}
}

type rolloutCall struct {
	method, path, origin, bearer, cookie string
	body                                 any
	headers                              map[string]string
	anonymous                            bool
}

func (f *rolloutRouteFixture) request(t *testing.T, input rolloutCall) *http.Request {
	t.Helper()
	var body []byte
	if raw, ok := input.body.([]byte); ok {
		body = raw
	} else if input.body != nil {
		var err error
		body, err = json.Marshal(input.body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(input.method, rolloutTestOrigin+input.path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if input.origin != "" {
		request.Header.Set("Origin", input.origin)
	}
	if !input.anonymous {
		token := input.cookie
		if token == "" {
			token = f.owner.Token
		}
		request.AddCookie(&http.Cookie{Name: handler.AdminSessionCookieName, Value: token})
	}
	if input.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+input.bearer)
	}
	for key, value := range input.headers {
		request.Header.Set(key, value)
	}
	return request
}

func (f *rolloutRouteFixture) call(t *testing.T, input rolloutCall) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	f.router.ServeHandler().ServeHTTP(response, f.request(t, input))
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cacheable rollout response", response.Code, input.path)
	}
	return response
}

func rolloutResponse[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var value struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value.Data
}

func (f *rolloutRouteFixture) node(t *testing.T) (string, string) {
	t.Helper()
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.NewNodeStore(f.db.DB).CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "rollout node", Host: "192.0.2.1", Port: 22}, store.AgentCredential{Hash: credential.Hash[:], Prefix: credential.Prefix, CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE nodes SET agent_version='1.0.0',os='linux',arch='amd64',last_seen=? WHERE id=?", f.now, node.ID); err != nil {
		t.Fatal(err)
	}
	return node.ID, credential.Plaintext
}

func rolloutCaps() protocol.Capabilities {
	return protocol.Capabilities{CurrentVersion: "1.0.0", OS: "linux", Arch: "amd64", Manager: "systemd", UpdateEnabled: true, AvailableBytes: 1 << 30, ProtocolVersion: model.AgentReportProtocol}
}
