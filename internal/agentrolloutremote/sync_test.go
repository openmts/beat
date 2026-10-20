package agentrolloutremote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type remoteFixture struct {
	service  *Service
	rollout  *agentrollout.Service
	actor    agentrollout.Actor
	manifest protocol.Manifest
	signed   protocol.SignedManifest
	release  apiRelease
	artifact []byte
	server   *httptest.Server
}

func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	actor := agentrollout.Actor{UserID: "owner", SessionID: "session", RequestID: "request", IPAddress: "127.0.0.1", UserAgent: "fixture"}
	seedRemoteOwner(t, database, actor, now)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rollout, err := agentrollout.New(agentrollout.Config{Database: database.DB, Directory: filepath.Join(t.TempDir(), "releases"),
		Keys:       []agentrollout.TrustedKey{{ID: "release", PublicKey: public, NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(365 * 24 * time.Hour)}},
		PinnedOnly: true, Enabled: true, ServerSchema: 15, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	artifact := []byte("verified agent binary")
	digest := sha256.Sum256(artifact)
	manifest := protocol.Manifest{SchemaVersion: 1, Version: "2.0.0", Commit: strings.Repeat("a", 40), Channel: "stable",
		BuiltAt: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour), MinimumServerSchema: 15, MinimumProtocol: 1, MaximumProtocol: 2,
		ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "release", Artifacts: []protocol.Artifact{{ID: "linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary",
			Filename: "beat-agent_linux_amd64", Entrypoint: "beat-agent_linux_amd64", Size: int64(len(artifact)), SHA256: hex.EncodeToString(digest[:]),
			SBOMDigest: strings.Repeat("c", 64), ProvenanceDigest: strings.Repeat("d", 64), Support: "supported"}}}
	payload, err := protocol.CanonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signed := protocol.SignedManifest{Payload: payload, Signature: ed25519.Sign(private, payload)}
	fixture := &remoteFixture{rollout: rollout, actor: actor, manifest: manifest, signed: signed, artifact: artifact}
	fixture.server = remoteReleaseServer(t, fixture, nil)
	service, err := New(Config{Rollout: rollout, APIURL: fixture.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	fixture.service = service
	return fixture
}

func seedRemoteOwner(t *testing.T, database *store.SQLiteStore, actor agentrollout.Actor, now time.Time) {
	t.Helper()
	admins := store.NewAdminStore(database.DB)
	user := model.AdminUser{ID: actor.UserID, Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true,
		PasswordHash: "fixture", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := admins.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	until := now.Add(24 * time.Hour)
	if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: actor.SessionID, UserID: user.ID, TokenHash: bytes.Repeat([]byte{1}, 32),
		TokenPrefix: "fixture", CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
}

func remoteReleaseServer(t *testing.T, fixture *remoteFixture, mutate func(int64, []byte) []byte) *httptest.Server {
	t.Helper()
	bodies := map[int64][]byte{1: fixture.signed.Payload, 2: fixture.signed.Signature, 3: []byte("sigstore"), 4: fixture.artifact}
	fixture.release = apiRelease{ID: 10, TagName: "v2.0.0", Name: "Beat 2.0", PublishedAt: time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC), Assets: []apiAsset{
		{ID: 1, Name: ManifestAssetName, Size: int64(len(bodies[1]))}, {ID: 2, Name: SignatureAssetName, Size: int64(len(bodies[2]))},
		{ID: 3, Name: SigstoreAssetName, Size: int64(len(bodies[3]))}, {ID: 4, Name: fixture.manifest.Artifacts[0].Filename, Size: int64(len(bodies[4]))},
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/releases/tags/v2.0.0"):
			_ = json.NewEncoder(w).Encode(fixture.release)
		case strings.Contains(request.URL.Path, "/releases/assets/"):
			var id int64
			_, _ = fmt.Sscanf(request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:], "%d", &id)
			body, ok := bodies[id]
			if !ok {
				http.NotFound(w, request)
				return
			}
			if mutate != nil {
				body = mutate(id, body)
			}
			_, _ = w.Write(body)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSyncImportsPinnedReleaseAndVerifiedArtifact(t *testing.T) {
	fixture := newRemoteFixture(t)
	detail, err := fixture.service.Sync(t.Context(), fixture.actor, "v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Verified || detail.Revoked || len(detail.Artifacts) != 1 || detail.Artifacts[0].State != "ready" || detail.Artifacts[0].SHA256 != fixture.manifest.Artifacts[0].SHA256 {
		t.Fatalf("detail = %+v", detail)
	}
	if _, err := fixture.service.Sync(t.Context(), fixture.actor, "v2.0.0"); err != nil {
		t.Fatalf("idempotent sync failed: %v", err)
	}
}

func TestSyncRejectsTamperedOrDraftRelease(t *testing.T) {
	fixture := newRemoteFixture(t)
	fixture.server.Close()
	fixture.server = remoteReleaseServer(t, fixture, func(id int64, body []byte) []byte {
		if id == 4 {
			return append(bytes.Clone(body), 'x')
		}
		return body
	})
	service, err := New(Config{Rollout: fixture.rollout, APIURL: fixture.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	fixture.service = service
	if _, err := fixture.service.Sync(t.Context(), fixture.actor, "v2.0.0"); !errors.Is(err, ErrInvalid) && !errors.Is(err, agentrollout.ErrManifest) && !errors.Is(err, agentrollout.ErrUnavailable) {
		t.Fatalf("tampered artifact = %v", err)
	}
	fixture.release.Draft = true
	if _, err := fixture.service.Sync(t.Context(), fixture.actor, "v2.0.0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft release = %v", err)
	}
	if _, err := fixture.service.Sync(context.Background(), fixture.actor, "latest"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid tag = %v", err)
	}
}
