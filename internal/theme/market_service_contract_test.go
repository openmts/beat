package theme

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type marketContractFixture struct {
	markets   *MarketService
	themes    *Service
	database  *store.SQLiteStore
	source    *model.ThemeMarketSource
	clock     atomic.Int64
	mode      atomic.Int32
	started   chan struct{}
	release   chan struct{}
	unblock   sync.Once
	requested sync.Once
	selection MarketSelection
	raw, body []byte
	signature string
}

func emptyThemeService(t *testing.T) (*Service, *store.SQLiteStore) {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return service, database
}

func newMarketContractFixture(t *testing.T, modify func(*model.ThemeCatalogEntry)) *marketContractFixture {
	t.Helper()
	fixture := &marketContractFixture{started: make(chan struct{}), release: make(chan struct{})}
	fixture.clock.Store(time.Now().UnixMilli())
	fixture.themes, fixture.database = emptyThemeService(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(public)
	fixture.raw = validPackageBytes(t)
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.serveContract))
	t.Cleanup(server.Close)
	t.Cleanup(func() { fixture.unblock.Do(func() { close(fixture.release) }) })
	entry := model.ThemeCatalogEntry{ThemeID: "acme-theme", Name: "Acme", Version: "1.0.0", Runtime: model.ThemeRuntimeDeclarative,
		PackageURL: server.URL + "/package", PackageSHA256: sha256Hex(fixture.raw), ManifestSHA256: marketTestManifestDigest(t, fixture.raw), MinimumBeat: "0.1.0"}
	if modify != nil {
		modify(&entry)
	}
	fixture.body, err = json.Marshal(model.ThemeCatalog{SchemaVersion: 1, SourceID: marketTestIdentity(t, public), ExpiresAt: fixture.now().Add(time.Hour), Themes: []model.ThemeCatalogEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, fixture.body))
	client := NewMarketClient(true, fixture.now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.SetRootCAs(pool)
	fixture.markets = NewMarketService(store.NewThemeStore(fixture.database.DB), client, fixture.themes, fixture.now)
	fixture.source, err = fixture.markets.CreateSource(t.Context(), &model.ThemeMarketSource{Name: "Market", CatalogURL: server.URL + "/catalog", PublicKey: key, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fixture.selection = MarketSelection{SourceID: fixture.source.ID, ThemeID: entry.ThemeID, Version: entry.Version, PackageSHA256: entry.PackageSHA256}
	return fixture
}

func (fixture *marketContractFixture) serveContract(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/package" {
		fixture.requested.Do(func() { close(fixture.started) })
		select {
		case <-fixture.release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(fixture.raw)
		return
	}
	if fixture.mode.Load() == 1 {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if fixture.mode.Load() == 2 {
		w.Header().Set(catalogSignatureHeader, "invalid")
	} else {
		w.Header().Set(catalogSignatureHeader, fixture.signature)
	}
	if fixture.mode.Load() == 0 && r.Header.Get("If-None-Match") == `"valid"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", `"valid"`)
	_, _ = w.Write(fixture.body)
}

func (fixture *marketContractFixture) now() time.Time {
	return time.UnixMilli(fixture.clock.Load()).UTC()
}

func TestMarketServiceCatalogCarriesIdentityAndExplicitCacheStatus(t *testing.T) {
	fixture := newMarketContractFixture(t, nil)
	first, err := fixture.markets.RefreshCatalog(t.Context(), fixture.source.ID)
	if err != nil || first.Cached || first.SourceID != fixture.source.KeyFingerprint {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := fixture.markets.RefreshCatalog(t.Context(), fixture.source.ID)
	if err != nil || !second.Cached || len(second.Themes) != 1 {
		t.Fatalf("304 lost body: %+v %v", second, err)
	}
	fixture.mode.Store(1)
	stale, err := fixture.markets.RefreshCatalog(t.Context(), fixture.source.ID)
	if err != nil || !stale.Stale || stale.ErrorCategory != "unavailable" {
		t.Fatalf("fallback: %+v %v", stale, err)
	}
	source, err := fixture.markets.store.GetMarketSource(t.Context(), fixture.source.ID)
	if err != nil || source.LastStatus != "stale" || source.CacheETag != `"valid"` {
		t.Fatalf("cache metadata: %+v %v", source, err)
	}
	fixture.mode.Store(2)
	if _, err := fixture.markets.RefreshCatalog(t.Context(), source.ID); !errors.Is(err, ErrMarketInvalid) {
		t.Fatal(err)
	}
	source, err = fixture.markets.store.GetMarketSource(t.Context(), source.ID)
	if err != nil || source.LastStatus != "failed" || source.ExpiresAt != nil || source.CatalogDigest != "" {
		t.Fatalf("untrusted cache retained: %+v %v", source, err)
	}
}

func TestMarketInstallBindsEverySignedPackageField(t *testing.T) {
	for _, field := range []string{"archive", "manifest", "id", "version", "runtime", "minimum", "maximum"} {
		t.Run(field, func(t *testing.T) {
			fixture := newMarketContractFixture(t, func(entry *model.ThemeCatalogEntry) {
				switch field {
				case "archive":
					entry.PackageSHA256 = sha256Hex([]byte("wrong"))
				case "manifest":
					entry.ManifestSHA256 = sha256Hex([]byte("wrong"))
				case "id":
					entry.ThemeID = "other-theme"
				case "version":
					entry.Version = "2.0.0"
				case "runtime":
					entry.Runtime = model.ThemeRuntimeSandboxed
				case "minimum":
					entry.MinimumBeat = "0.0.9"
				case "maximum":
					entry.MaximumBeat = "1.0.0"
				}
			})
			fixture.unblock.Do(func() { close(fixture.release) })
			if _, err := fixture.markets.InstallFromMarket(t.Context(), fixture.selection, "owner"); !errors.Is(err, ErrMarketInvalid) {
				t.Fatalf("signed mismatch accepted: %v", err)
			}
			packages, err := fixture.themes.ListPackages(t.Context())
			if err != nil || len(packages) != 0 {
				t.Fatalf("partial install: %+v %v", packages, err)
			}
		})
	}
}

func TestMarketInstallRejectsRevocationWhileDownloading(t *testing.T) {
	for _, change := range []string{"disable", "delete", "key", "url", "expiry"} {
		t.Run(change, func(t *testing.T) {
			fixture := newMarketContractFixture(t, nil)
			completed := make(chan error, 1)
			go func() {
				_, err := fixture.markets.InstallFromMarket(t.Context(), fixture.selection, "owner")
				completed <- err
			}()
			select {
			case <-fixture.started:
			case <-time.After(3 * time.Second):
				t.Fatal("download did not start")
			}
			mutateMarketDuringDownload(t, fixture, change)
			fixture.unblock.Do(func() { close(fixture.release) })
			if err := <-completed; err == nil {
				t.Fatal("source revocation was ignored")
			}
			packages, err := fixture.themes.ListPackages(t.Context())
			if err != nil || len(packages) != 0 {
				t.Fatalf("revoked package persisted: %+v %v", packages, err)
			}
			entries, err := os.ReadDir(filepath.Join(fixture.themes.root, "packages"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("revoked install left assets: %+v %v", entries, err)
			}
		})
	}
}

func mutateMarketDuringDownload(t *testing.T, fixture *marketContractFixture, change string) {
	t.Helper()
	input := *fixture.source
	input.KeyFingerprint = ""
	switch change {
	case "delete":
		if err := fixture.markets.DeleteSource(t.Context(), input.ID); err != nil {
			t.Fatal(err)
		}
		return
	case "expiry":
		fixture.clock.Add(int64(16 * time.Minute / time.Millisecond))
		return
	case "disable":
		input.Enabled = false
	case "key":
		input.PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	case "url":
		input.CatalogURL += "/changed"
	}
	if _, err := fixture.markets.UpdateSource(t.Context(), input.ID, input); err != nil {
		t.Fatal(err)
	}
}

func TestMarketSourceChangesClearCacheAndNeverTrustInputFingerprint(t *testing.T) {
	fixture := newMarketContractFixture(t, nil)
	if _, err := fixture.markets.RefreshCatalog(t.Context(), fixture.source.ID); err != nil {
		t.Fatal(err)
	}
	input := *fixture.source
	input.KeyFingerprint = "sha256:untrusted"
	if _, err := fixture.markets.UpdateSource(t.Context(), input.ID, input); !errors.Is(err, ErrMarketInvalid) {
		t.Fatal(err)
	}
	input.KeyFingerprint, input.Enabled = "", false
	changed, err := fixture.markets.UpdateSource(t.Context(), input.ID, input)
	if err != nil || changed.CatalogDigest != "" || changed.ExpiresAt != nil {
		t.Fatalf("old cache survived: %+v %v", changed, err)
	}
	fixture.mode.Store(1)
	changed.Enabled = true
	if _, err := fixture.markets.UpdateSource(t.Context(), input.ID, *changed); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.markets.RefreshCatalog(t.Context(), input.ID); !errors.Is(err, ErrMarketUnavailable) {
		t.Fatalf("re-enabled source used old LKG: %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.markets.UpdateSource(canceled, input.ID, input); err == nil {
		t.Fatal("canceled change succeeded")
	}
}
