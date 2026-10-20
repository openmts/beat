package theme

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestMarketPublicIdentityAndSignedExpiry(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(public)
	identity, err := MarketIdentity(key)
	if err != nil || !strings.HasPrefix(identity, "sha256:") || len(identity) != 71 {
		t.Fatalf("identity: %s %v", identity, err)
	}
	now := time.Now().UTC()
	catalog := model.ThemeCatalog{SchemaVersion: 1, SourceID: identity, ExpiresAt: now.Add(time.Hour), Themes: []model.ThemeCatalogEntry{}}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, body))
	result, err := verifyMarketCatalog(body, signature, catalogVerification{publicKey: key, now: now})
	if err != nil || result.SourceID != identity {
		t.Fatalf("valid shared source: %+v %v", result, err)
	}
	for _, mutate := range []func(*model.ThemeCatalog){
		func(value *model.ThemeCatalog) { value.SourceID = "local-installation-uuid" },
		func(value *model.ThemeCatalog) { value.ExpiresAt = now },
		func(value *model.ThemeCatalog) { value.ExpiresAt = now.Add(8 * 24 * time.Hour) },
		func(value *model.ThemeCatalog) { value.SchemaVersion = 2 },
	} {
		candidate := catalog
		mutate(&candidate)
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		signed := base64.StdEncoding.EncodeToString(ed25519.Sign(private, raw))
		if _, err := verifyMarketCatalog(raw, signed, catalogVerification{publicKey: key, now: now}); err == nil {
			t.Fatal("invalid signed catalog accepted")
		}
	}
}

func TestCatalogEntryRequiresBothDigestsAndSafeDownloadURL(t *testing.T) {
	entry := model.ThemeCatalogEntry{ThemeID: "a-theme", Name: "A", Version: "1.0.0", Author: "Author", Description: "Example", Runtime: model.ThemeRuntimeDeclarative,
		PackageURL: "https://market.example/a.zip", PackageSHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64), MinimumBeat: "0.1.0"}
	if err := validateCatalogEntry(entry, false); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*model.ThemeCatalogEntry){
		func(value *model.ThemeCatalogEntry) { value.ManifestSHA256 = "" }, func(value *model.ThemeCatalogEntry) { value.PackageSHA256 = "wrong" },
		func(value *model.ThemeCatalogEntry) { value.PackageURL = "https://127.0.0.1/private" }, func(value *model.ThemeCatalogEntry) { value.Runtime = "arbitrary" },
	} {
		candidate := entry
		mutate(&candidate)
		if err := validateCatalogEntry(candidate, false); err == nil {
			t.Fatal("invalid catalog entry accepted")
		}
	}
}
