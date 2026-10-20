package theme

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestMarketSelectionCannotSilentlyInstallAnUpdatedCatalogEntry(t *testing.T) {
	for _, mismatch := range []string{"version", "digest", "withdrawn", "missing selection"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := newMarketContractFixture(t, nil)
			selection := fixture.selection
			want := ErrMarketChanged
			switch mismatch {
			case "version":
				selection.Version = "9.0.0"
			case "digest":
				selection.PackageSHA256 = sha256Hex([]byte("old package"))
			case "withdrawn":
				selection.ThemeID = "removed-theme"
			case "missing selection":
				selection.Version = ""
				want = ErrMarketInvalid
			}
			if _, err := fixture.markets.InstallFromMarket(t.Context(), selection, "owner"); !errors.Is(err, want) {
				t.Fatalf("selection mismatch accepted: %v", err)
			}
			select {
			case <-fixture.started:
				t.Fatal("mismatched package was downloaded")
			default:
			}
		})
	}
}

func TestMarketInstallRechecksLiveActorAfterDownload(t *testing.T) {
	for _, revoked := range []string{"account", "session", "proof"} {
		t.Run(revoked, func(t *testing.T) {
			fixture := newMarketContractFixture(t, nil)
			actor := marketOwnerContext(t, fixture.database)
			completed := make(chan error, 1)
			go func() {
				_, err := fixture.markets.InstallFromMarket(actor, fixture.selection, "owner")
				completed <- err
			}()
			select {
			case <-fixture.started:
			case <-time.After(3 * time.Second):
				t.Fatal("download did not start")
			}
			queries := map[string]string{"account": `UPDATE admin_users SET enabled=0`, "session": `UPDATE admin_sessions SET revoked_at='2000-01-01'`, "proof": `UPDATE admin_sessions SET reauthenticated_until=NULL`}
			if _, err := fixture.database.DB.Exec(queries[revoked]); err != nil {
				t.Fatal(err)
			}
			fixture.unblock.Do(func() { close(fixture.release) })
			err := <-completed
			if !errors.Is(err, store.ErrThemeForbidden) && !errors.Is(err, store.ErrThemeRecentAuth) {
				t.Fatalf("live authority not checked: %v", err)
			}
			packages, err := fixture.themes.ListPackages(t.Context())
			if err != nil || len(packages) != 0 {
				t.Fatalf("revoked actor installed package: %+v %v", packages, err)
			}
		})
	}
}

func marketOwnerContext(t *testing.T, database *store.SQLiteStore) context.Context {
	t.Helper()
	users := store.NewAdminStore(database.DB)
	now, expiry := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
	user := model.AdminUser{ID: "owner", Username: "market-owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true, CreatedAt: now, UpdatedAt: now, PasswordChangedAt: now}
	if err := users.CreateUser(t.Context(), &user); err != nil {
		t.Fatal(err)
	}
	session := model.AdminSession{ID: "market-session", UserID: user.ID, TokenHash: make([]byte, 32), CreatedAt: now, LastActivityAt: now,
		IdleExpiresAt: expiry, AbsoluteExpiresAt: expiry, ReauthenticatedUntil: &expiry, AuthenticationMethod: "local"}
	if err := users.CreateSession(t.Context(), &session); err != nil {
		t.Fatal(err)
	}
	return store.WithThemeActor(t.Context(), store.ThemeActor{UserID: user.ID, SessionID: session.ID})
}

func TestCatalogCacheHasABoundedCapacityAndDoesNotReuseChangedKey(t *testing.T) {
	fixture := newCatalogFixture(t)
	for index := range maximumCachedCatalogs + 1 {
		source := *fixture.source
		source.ID += string(rune('a' + index))
		if _, err := fixture.client.fetchCatalog(t.Context(), &source, false); err != nil {
			t.Fatal(err)
		}
	}
	if len(fixture.client.catalogs) != maximumCachedCatalogs {
		t.Fatalf("unbounded catalog cache: %d", len(fixture.client.catalogs))
	}
	if err := fixture.client.invalidateSource(t.Context(), fixture.source.ID+"b"); err != nil {
		t.Fatal(err)
	}
	if len(fixture.client.catalogs) != maximumCachedCatalogs-1 {
		t.Fatal("source invalidation missed cache entry")
	}
	fixture.source.ID += "c"
	fixture.source.PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := fixture.client.fetchCatalog(t.Context(), fixture.source, false); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("changed key reused cached trust: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := fixture.client.invalidateSource(ctx, fixture.source.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
