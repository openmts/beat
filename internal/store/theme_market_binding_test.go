package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func themeSourceFixture(t *testing.T, themes *ThemeStore) *model.ThemeMarketSource {
	t.Helper()
	source := &model.ThemeMarketSource{ID: "source", Name: "Publisher", CatalogURL: "https://example.com/catalog", Enabled: true,
		PublicKey: repeat('k', 44), KeyFingerprint: "fingerprint", CatalogDigest: repeat('a', 64), CreatedAt: model.NowUTC(), UpdatedAt: model.NowUTC()}
	if err := themes.CreateMarketSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestThemeMarketFinalWriteRevalidatesTrustBinding(t *testing.T) {
	for _, mismatch := range []string{"missing", "disabled", "url", "key", "digest", "expired", "valid"} {
		t.Run(mismatch, func(t *testing.T) {
			themes, _ := setupThemeStore(t)
			source := themeSourceFixture(t, themes)
			binding := ThemeMarketBinding{SourceID: source.ID, CatalogURL: source.CatalogURL, PublicKey: source.PublicKey, CatalogDigest: source.CatalogDigest, ExpiresAt: model.NowUTC().Add(time.Hour)}
			switch mismatch {
			case "missing":
				binding.SourceID = "missing"
			case "disabled":
				source.Enabled = false
			case "url":
				binding.CatalogURL += "/changed"
			case "key":
				binding.PublicKey = repeat('z', 44)
			case "digest":
				binding.CatalogDigest = repeat('b', 64)
			case "expired":
				binding.Now = func() time.Time { return binding.ExpiresAt }
			}
			if err := themes.UpdateMarketSource(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			ctx := WithThemeMarketBinding(t.Context(), binding)
			err := themes.CreatePackage(ctx, sampleThemePackage("selected"))
			if mismatch == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrThemeMarketSourceID) {
				t.Fatalf("source binding bypass: %v", err)
			}
			if err := themes.RestorePackage(ctx, sampleThemePackage("selected")); !errors.Is(err, ErrThemeMarketSourceID) {
				t.Fatalf("restore bypass: %v", err)
			}
		})
	}
}

func TestThemeMarketObservationCannotReenableOrOverwriteChangedSource(t *testing.T) {
	themes, _ := setupThemeStore(t)
	source := themeSourceFixture(t, themes)
	observation := *source
	observation.LastStatus, observation.CacheETag = "ok", `"v1"`
	if err := themes.UpdateMarketObservation(t.Context(), &observation); err != nil {
		t.Fatal(err)
	}
	source.Enabled = false
	if err := themes.UpdateMarketSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if err := themes.UpdateMarketObservation(t.Context(), &observation); !errors.Is(err, ErrThemeMarketSourceID) {
		t.Fatalf("stale refresh overwrote revocation: %v", err)
	}
	current, err := themes.GetMarketSource(t.Context(), source.ID)
	if err != nil || current.Enabled {
		t.Fatalf("source reenabled: %+v %v", current, err)
	}
}

func TestThemeMarketMutationAuditFailureRollsBackSource(t *testing.T) {
	themes, database := setupThemeStore(t)
	source := themeSourceFixture(t, themes)
	if _, err := database.DB.Exec(`CREATE TRIGGER market_audit_offline BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	source.Enabled = false
	if err := themes.UpdateMarketSource(t.Context(), source); err == nil {
		t.Fatal("unaudited source disable succeeded")
	}
	if err := themes.DeleteMarketSource(t.Context(), source.ID); err == nil {
		t.Fatal("unaudited source deletion succeeded")
	}
	current, err := themes.GetMarketSource(t.Context(), source.ID)
	if err != nil || !current.Enabled {
		t.Fatalf("partial source mutation: %+v %v", current, err)
	}
}
