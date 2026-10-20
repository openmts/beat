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
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type catalogFixture struct {
	client     *MarketClient
	source     *model.ThemeMarketSource
	now        time.Time
	mode       atomic.Int32
	requests   atomic.Int32
	conditions atomic.Int32
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &catalogFixture{now: time.Now().UTC()}
	key := base64.StdEncoding.EncodeToString(public)
	identity, err := MarketIdentity(key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(model.ThemeCatalog{SchemaVersion: 1, SourceID: identity, ExpiresAt: fixture.now.Add(2 * time.Hour), Themes: []model.ThemeCatalogEntry{}})
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, body))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		if r.Header.Get("If-None-Match") != "" {
			fixture.conditions.Add(1)
		}
		if fixture.mode.Load() == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if fixture.mode.Load() == 3 || r.Header.Get("If-None-Match") == `"catalog-one"` && fixture.mode.Load() != 2 {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"catalog-one"`)
		w.Header().Set("Last-Modified", "Sun, 13 Sep 2026 00:00:00 GMT")
		w.Header().Set(catalogSignatureHeader, signature)
		if fixture.mode.Load() == 2 {
			w.Header().Set(catalogSignatureHeader, "invalid")
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	fixture.source = &model.ThemeMarketSource{ID: "local-source", CatalogURL: server.URL, PublicKey: key, KeyFingerprint: identity, Enabled: true}
	fixture.client = NewMarketClient(true, func() time.Time { return fixture.now })
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	fixture.client.SetRootCAs(pool)
	return fixture
}

func TestCatalogCacheReturnsVerifiedBodyAndHandles304(t *testing.T) {
	fixture := newCatalogFixture(t)
	first, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false)
	if err != nil || first.Cached || first.Stale || first.SourceID != fixture.source.KeyFingerprint {
		t.Fatalf("initial catalog: %+v %v", first, err)
	}
	second, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false)
	if err != nil || !second.Cached || second.Digest != first.Digest || fixture.requests.Load() != 1 {
		t.Fatalf("cached catalog: %+v %v", second, err)
	}
	fixture.now = fixture.now.Add(time.Minute)
	refreshed, err := fixture.client.fetchCatalog(t.Context(), fixture.source, true)
	if err != nil || refreshed == nil || refreshed.Stale || !refreshed.FetchedAt.Equal(fixture.now) || fixture.conditions.Load() != 1 {
		t.Fatalf("304 catalog: %+v %v", refreshed, err)
	}
}

func TestCatalogCacheFallbackIsExplicitAndBounded(t *testing.T) {
	fixture := newCatalogFixture(t)
	first, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false)
	if err != nil {
		t.Fatal(err)
	}
	fixture.mode.Store(1)
	fixture.now = fixture.now.Add(5 * time.Minute)
	stale, err := fixture.client.fetchCatalog(t.Context(), fixture.source, true)
	if err != nil || !stale.Stale || stale.ErrorCategory != "unavailable" || !stale.CacheExpiresAt.Equal(first.CacheExpiresAt) {
		t.Fatalf("stale fallback: %+v %v", stale, err)
	}
	fixture.now = fixture.now.Add(11 * time.Minute)
	if value, err := fixture.client.fetchCatalog(t.Context(), fixture.source, true); !errors.Is(err, ErrMarketUnavailable) || value != nil {
		t.Fatalf("expired cache served: %+v %v", value, err)
	}
}

func TestCatalogUntrusted304AndInvalidSignatureNeverBecomeSuccessfulCache(t *testing.T) {
	fixture := newCatalogFixture(t)
	fixture.source.CacheETag = `"catalog-one"`
	fixture.mode.Store(3)
	if value, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false); !errors.Is(err, ErrMarketInvalid) || value != nil || fixture.conditions.Load() != 0 {
		t.Fatalf("untrusted validator: %+v %v", value, err)
	}
	fixture.mode.Store(0)
	if _, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false); err != nil {
		t.Fatal(err)
	}
	fixture.mode.Store(2)
	if value, err := fixture.client.fetchCatalog(t.Context(), fixture.source, true); !errors.Is(err, ErrMarketInvalid) || value != nil {
		t.Fatalf("invalid signature used LKG: %+v %v", value, err)
	}
	fixture.mode.Store(1)
	if value, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false); !errors.Is(err, ErrMarketUnavailable) || value != nil {
		t.Fatalf("invalid cache retained: %+v %v", value, err)
	}
}

func TestCatalogCacheBindingExpiryAndCancellation(t *testing.T) {
	fixture := newCatalogFixture(t)
	if _, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false); err != nil {
		t.Fatal(err)
	}
	fixture.source.CatalogURL += "/new"
	if _, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false); err != nil || fixture.conditions.Load() != 0 {
		t.Fatalf("URL reused validator: %v", err)
	}
	fixture.now = fixture.now.Add(3 * time.Hour)
	fixture.mode.Store(3)
	if _, err := fixture.client.fetchCatalog(t.Context(), fixture.source, true); err == nil {
		t.Fatal("signed expiry ignored")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.client.fetchCatalog(canceled, fixture.source, true); err == nil {
		t.Fatal("cancellation ignored")
	}
}
