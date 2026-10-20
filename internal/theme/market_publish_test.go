package theme

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestPublishedCatalogVerifiesWithPublicIdentityAndRawDigests(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := validPackageBytes(t)
	second := buildThemeZip(t, sampleManifest(t, map[string]any{"id": "before-acme"}), map[string]string{"preview.png": fixturePNG, "assets/theme.css": "c"})
	now := time.Now().UTC()
	published, err := BuildMarketCatalog(MarketPublishOptions{PrivateKey: private, Now: now, ExpiresAt: now.Add(time.Hour),
		Packages: []MarketPackageInput{{Archive: second, URL: "https://example.com/b.zip"}, {Archive: archive, URL: "https://example.com/a.zip"}}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := verifyMarketCatalog(published.Body, published.Signature, catalogVerification{publicKey: base64.StdEncoding.EncodeToString(public), now: now})
	if err != nil {
		t.Fatal(err)
	}
	if published.SourceID != marketTestIdentity(t, public) || catalog.SourceID != published.SourceID || catalog.Themes[0].ThemeID != "acme-theme" {
		t.Fatalf("wrong identity or order: %+v", catalog)
	}
	entry := catalog.Themes[0]
	if entry.PackageSHA256 != sha256Hex(archive) || entry.ManifestSHA256 != marketTestManifestDigest(t, archive) {
		t.Fatalf("wrong signed bytes: %+v", entry)
	}
	if _, err := verifyMarketCatalog(append(published.Body, ' '), published.Signature, catalogVerification{publicKey: base64.StdEncoding.EncodeToString(public), now: now}); !errors.Is(err, ErrMarketInvalid) {
		t.Fatal("modified body verified")
	}
}

func TestCatalogPublisherRejectsInvalidIdentityLifetimeAndPackages(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := validPackageBytes(t)
	now := time.Now().UTC()
	for _, name := range []string{"missing key", "inconsistent key", "expired", "too long", "invalid archive", "private URL", "duplicate", "too many"} {
		t.Run(name, func(t *testing.T) {
			options := MarketPublishOptions{PrivateKey: append(ed25519.PrivateKey(nil), private...), Now: now, ExpiresAt: now.Add(time.Hour), Packages: []MarketPackageInput{{Archive: archive, URL: "https://example.com/a.zip"}}}
			switch name {
			case "missing key":
				options.PrivateKey = nil
			case "inconsistent key":
				options.PrivateKey[len(options.PrivateKey)-1] ^= 1
			case "expired":
				options.ExpiresAt = now
			case "too long":
				options.ExpiresAt = now.Add(8 * 24 * time.Hour)
			case "invalid archive":
				options.Packages[0].Archive = []byte("bad")
			case "private URL":
				options.Packages[0].URL = "https://127.0.0.1/a.zip"
			case "duplicate":
				options.Packages = append(options.Packages, options.Packages[0])
			case "too many":
				options.Packages = make([]MarketPackageInput, maximumCatalogEntries+1)
			}
			if _, err := BuildMarketCatalog(options); err == nil {
				t.Fatal("invalid publish succeeded")
			}
		})
	}
}
