package agentrolloutremote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout"
	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestSignedManifestRequiresSigstoreAndBoundsBundles(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := protocol.Manifest{SchemaVersion: 1, Version: "1.0.0", Commit: strings.Repeat("a", 40), Channel: "stable",
		BuiltAt: timeDate(), ExpiresAt: timeDate().Add(24 * time.Hour), MinimumServerSchema: 15, MinimumProtocol: 1, MaximumProtocol: 2,
		ReleaseNotesDigest: strings.Repeat("b", 64), SigningKeyID: "release", Artifacts: []protocol.Artifact{{ID: "linux-amd64", OS: "linux", Arch: "amd64", Kind: "binary",
			Filename: "agent", Entrypoint: "agent", Size: 1, SHA256: strings.Repeat("c", 64), SBOMDigest: strings.Repeat("d", 64), ProvenanceDigest: strings.Repeat("e", 64), Support: "supported"}}}
	payload, _ := protocol.CanonicalManifest(manifest)
	signature := ed25519.Sign(private, payload)
	_ = public
	for name, assets := range map[string]map[string][]byte{
		"complete":         {ManifestAssetName: payload, SignatureAssetName: signature, SigstoreAssetName: []byte("bundle")},
		"missing bundle":   {ManifestAssetName: payload, SignatureAssetName: signature},
		"oversized bundle": {ManifestAssetName: payload, SignatureAssetName: signature, SigstoreAssetName: make([]byte, (4<<20)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			var release apiRelease
			server := assetServer(t, assets, func(value *apiRelease) { release = *value })
			defer server.Close()
			service := &Service{rollout: &agentrollout.Service{}, api: mustURL(t, server.URL), client: server.Client()}
			signed, err := service.signedManifest(context.Background(), release)
			if name == "complete" {
				if err != nil || string(signed.SigstoreBundle) != "bundle" || !ed25519.Verify(public, signed.Payload, signed.Signature) {
					t.Fatalf("signed = %+v, %v", signed, err)
				}
			} else if !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid bundle case = %v", err)
			}
		})
	}
}

func TestRedirectsCancellationAndRejectedReleaseMetadata(t *testing.T) {
	service := &Service{api: mustURL(t, "https://api.github.com"), slots: make(chan struct{}, 1)}
	target := mustURL(t, "https://github.com/openmts/beat")
	if err := service.allowRedirect(&http.Request{URL: target}, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.allowRedirect(&http.Request{URL: mustURL(t, "http://evil.example")}, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unsafe redirect = %v", err)
	}
	service.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.acquire(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("canceled acquire = %v", err)
	}
	if (&RateLimitError{}).Error() != ErrRateLimited.Error() {
		t.Fatal("rate limit error string is unstable")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(apiRelease{ID: 1, TagName: "v9.9.9"})
	}))
	defer server.Close()
	invalid := &Service{rollout: &agentrollout.Service{}, api: mustURL(t, server.URL), client: server.Client()}
	if _, err := invalid.releaseByTag(context.Background(), "v1.0.0"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched release = %v", err)
	}
	zeroServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(apiRelease{TagName: "v1.0.0"})
	}))
	defer zeroServer.Close()
	invalid.api, invalid.client = mustURL(t, zeroServer.URL), zeroServer.Client()
	if _, err := invalid.releaseByTag(context.Background(), "v1.0.0"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero release ID = %v", err)
	}
}

func assetServer(t *testing.T, assets map[string][]byte, capture func(*apiRelease)) *httptest.Server {
	t.Helper()
	release := apiRelease{ID: 1, TagName: "v1.0.0"}
	for index, name := range []string{ManifestAssetName, SignatureAssetName, SigstoreAssetName} {
		if body, exists := assets[name]; exists {
			release.Assets = append(release.Assets, apiAsset{ID: int64(index + 1), Name: name, Size: int64(len(body))})
		}
	}
	if capture != nil {
		capture(&release)
	}
	byID := map[string][]byte{}
	for index, name := range []string{ManifestAssetName, SignatureAssetName, SigstoreAssetName} {
		if body, exists := assets[name]; exists {
			byID[decimal(int64(index+1))] = body
		}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		id := request.URL.Path[strings.LastIndex(request.URL.Path, "/")+1:]
		body, exists := byID[id]
		if !exists {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write(body)
	}))
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func timeDate() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }

func TestAssetAndArtifactValidationRejectsAmbiguousInput(t *testing.T) {
	var release apiRelease
	server := assetServer(t, map[string][]byte{ManifestAssetName: []byte("manifest"), SignatureAssetName: make([]byte, 65)}, func(value *apiRelease) { release = *value })
	defer server.Close()
	service := &Service{rollout: &agentrollout.Service{}, api: mustURL(t, server.URL), client: server.Client(), slots: make(chan struct{}, 1)}
	if _, err := service.signedManifest(context.Background(), release); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short signature = %v", err)
	}
	release.Assets[1].Size = 64
	release.Assets = append(release.Assets, apiAsset{ID: 3, Name: SignatureAssetName, Size: 64})
	if _, err := service.signedManifest(context.Background(), release); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate signature = %v", err)
	}
	artifact := protocol.Artifact{Filename: "agent", Size: 1}
	release.Assets = []apiAsset{{ID: 1, Name: "agent", Size: 1}, {ID: 2, Name: "agent", Size: 1}}
	if err := service.syncArtifact(context.Background(), agentrollout.Actor{}, release, "release", artifact); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate artifact = %v", err)
	}
}

func TestReadAssetRejectsStatusAndIncorrectLengths(t *testing.T) {
	for name, testCase := range map[string]struct {
		handler http.HandlerFunc
		size    int64
	}{
		"status": {handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, size: 4},
		"short":  {handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) }, size: 4},
		"long":   {handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("long")) }, size: 2},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(testCase.handler)
			defer server.Close()
			service := &Service{api: mustURL(t, server.URL), client: server.Client()}
			_, err := service.readAsset(context.Background(), apiAsset{ID: 1, Name: "asset", Size: testCase.size}, 8)
			if !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrInvalid) {
				t.Fatalf("asset error = %v", err)
			}
		})
	}
}

func TestReleaseByTagRejectsExcessAssets(t *testing.T) {
	assets := make([]apiAsset, maximumRemoteAssets+1)
	for index := range assets {
		assets[index] = apiAsset{ID: int64(index + 1), Name: "asset-" + decimal(int64(index))}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(apiRelease{ID: 1, TagName: "v1.0.0", Assets: assets})
	}))
	defer server.Close()
	service := &Service{rollout: &agentrollout.Service{}, api: mustURL(t, server.URL), client: server.Client()}
	if _, err := service.releaseByTag(context.Background(), "v1.0.0"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("excess assets = %v", err)
	}
}

func TestSyncPropagatesSourceAndSignatureFailures(t *testing.T) {
	fixture := newRemoteFixture(t)
	fixture.server.Close()
	fixture.server = remoteReleaseServer(t, fixture, func(id int64, body []byte) []byte {
		if id == 2 {
			changed := bytes.Clone(body)
			changed[0] ^= 1
			return changed
		}
		return body
	})
	service, err := New(Config{Rollout: fixture.rollout, APIURL: fixture.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(t.Context(), fixture.actor, "v2.0.0"); !errors.Is(err, agentrollout.ErrSignature) {
		t.Fatalf("tampered signature = %v", err)
	}
	if _, err := service.Sync(t.Context(), fixture.actor, "v1.2.3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing release = %v", err)
	}
	artifact := protocol.Artifact{Filename: "agent", Size: 2}
	release := apiRelease{Assets: []apiAsset{{ID: 1, Name: "agent", Size: 1}}}
	if err := fixture.service.syncArtifact(t.Context(), fixture.actor, release, "release", artifact); !errors.Is(err, ErrInvalid) {
		t.Fatalf("artifact size mismatch = %v", err)
	}
}
