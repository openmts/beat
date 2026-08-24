package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func setupThemeStore(t *testing.T) (*ThemeStore, *SQLiteStore) {
	t.Helper()
	sqlite := setupTestDB(t)
	return NewThemeStore(sqlite.DB), sqlite
}

func sampleThemePackage(id string) *model.ThemePackage {
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(id))
	return &model.ThemePackage{
		ID: id, ThemeID: "theme-" + id, Name: "Theme " + id, Version: "1.0.0",
		Author: "author", Description: "desc", License: "MIT",
		Runtime: model.ThemeRuntimeDeclarative, ManifestSchema: 1,
		MinimumBeatVersion: "0.0.0", ArchiveDigest: fmt.Sprintf("%x", digest),
		ArchiveBytes: 100, ContentBytes: 200, ManifestJSON: `{"files":[]}`,
		ConfigurationJSON: "{}", SourceType: model.ThemeSourceUpload,
		State: model.ThemeStateInstalled, ValidatedAt: now, CreatorID: "owner-1",
		CreatedAt: now, UpdatedAt: now,
	}
}

func repeat(character rune, count int) string {
	out := make([]byte, count)
	for index := range out {
		out[index] = byte(character)
	}
	return string(out)
}

func TestThemePackageCRUDAndConflicts(t *testing.T) {
	store, _ := setupThemeStore(t)
	ctx := context.Background()

	first := sampleThemePackage("p1")
	first.ThemeID = "corporate"
	if err := store.CreatePackage(ctx, first); err != nil {
		t.Fatalf("create package: %v", err)
	}
	// 相同 (theme_id, version) 冲突
	conflict := sampleThemePackage("p2")
	conflict.ThemeID = "corporate"
	conflict.ArchiveDigest = repeat('b', 64)
	if err := store.CreatePackage(ctx, conflict); err == nil {
		t.Fatal("duplicate (theme_id, version) must fail")
	}
	// 相同 digest 冲突
	digestConflict := sampleThemePackage("p3")
	digestConflict.ThemeID = "other"
	firstDigest := sha256.Sum256([]byte("p1"))
	digestConflict.ArchiveDigest = fmt.Sprintf("%x", firstDigest)
	if err := store.CreatePackage(ctx, digestConflict); err == nil {
		t.Fatal("duplicate archive digest must fail")
	}

	got, err := store.GetPackageByID(ctx, "p1")
	if err != nil || got == nil || got.ThemeID != "corporate" {
		t.Fatalf("get package = %v / %v", got, err)
	}
	byDigest, err := store.GetPackageByDigest(ctx, fmt.Sprintf("%x", sha256.Sum256([]byte("p1"))))
	if err != nil || byDigest == nil {
		t.Fatalf("get by digest = %v / %v", byDigest, err)
	}
	missing, err := store.GetPackageByID(ctx, "missing")
	if err != nil || missing != nil {
		t.Fatalf("missing package = %v / %v", missing, err)
	}

	listed, err := store.ListPackages(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list packages = %d / %v", len(listed), err)
	}
	if err := store.UpdatePackageState(ctx, "p1", model.ThemeStateQuarantined, "content_missing", time.Now().UTC()); err != nil {
		t.Fatalf("update state: %v", err)
	}
	if err := store.UpdatePackageState(ctx, "missing", model.ThemeStateQuarantined, "x", time.Now().UTC()); !errors.Is(err, ErrThemePackageNotFound) {
		t.Fatalf("update missing = %v", err)
	}
}

func TestThemeActivationSingleCurrent(t *testing.T) {
	store, _ := setupThemeStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := store.CreatePackage(ctx, sampleThemePackage("p1")); err != nil {
		t.Fatalf("create p1: %v", err)
	}
	if err := store.CreatePackage(ctx, sampleThemePackage("p2")); err != nil {
		t.Fatalf("create p2: %v", err)
	}

	if err := store.ActivatePackage(ctx, &model.ThemeActivation{
		ID: "act-1", PackageID: "p1", ActorID: "owner-1",
		Reason: model.ThemeActivationReason, ActivatedAt: now,
	}, now); err != nil {
		t.Fatalf("activate p1: %v", err)
	}
	activation, err := store.CurrentActivation(ctx)
	if err != nil || activation == nil || activation.PackageID != "p1" {
		t.Fatalf("current activation = %v / %v", activation, err)
	}

	if err := store.ActivatePackage(ctx, &model.ThemeActivation{
		ID: "act-2", PackageID: "p2", PreviousPackageID: "p1", ActorID: "owner-1",
		Reason: model.ThemeActivationReason, ActivatedAt: now.Add(time.Second),
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("activate p2: %v", err)
	}
	activation, _ = store.CurrentActivation(ctx)
	if activation.PackageID != "p2" {
		t.Fatalf("current activation = %#v, want p2", activation)
	}

	if err := store.MarkActivationUnhealthy(ctx, "iframe_timeout", now.Add(2*time.Second)); err != nil {
		t.Fatalf("mark unhealthy: %v", err)
	}
	activation, _ = store.CurrentActivation(ctx)
	if activation.HealthState != "unhealthy" || activation.LastFailureCategory != "iframe_timeout" {
		t.Fatalf("activation health = %#v", activation)
	}
}

func TestThemeMarketSourceCRUD(t *testing.T) {
	store, _ := setupThemeStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	source := &model.ThemeMarketSource{
		ID: "m1", Name: "Market", CatalogURL: "https://market.example/catalog.json",
		Enabled: true, SortOrder: 0, PublicKey: repeat('k', 44),
		KeyFingerprint: repeat('f', 16), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateMarketSource(ctx, source); err != nil {
		t.Fatalf("create market: %v", err)
	}
	got, err := store.GetMarketSource(ctx, "m1")
	if err != nil || got == nil || got.Name != "Market" {
		t.Fatalf("get market = %v / %v", got, err)
	}
	source.Name = "Renamed"
	if err := store.UpdateMarketSource(ctx, source); err != nil {
		t.Fatalf("update market: %v", err)
	}
	refreshed, _ := store.GetMarketSource(ctx, "m1")
	if refreshed.Name != "Renamed" {
		t.Fatalf("updated market = %#v", refreshed)
	}
	listed, err := store.ListMarketSources(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list markets = %d / %v", len(listed), err)
	}
	if err := store.DeleteMarketSource(ctx, "m1"); err != nil {
		t.Fatalf("delete market: %v", err)
	}
	if err := store.DeleteMarketSource(ctx, "m1"); !errors.Is(err, ErrThemeMarketNotFound) {
		t.Fatalf("delete missing = %v", err)
	}
}

func TestThemeStoreErrorPathsWithClosedDatabase(t *testing.T) {
	store, sqlite := setupThemeStore(t)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	pkg := sampleThemePackage("p1")
	source := &model.ThemeMarketSource{
		ID: "m1", Name: "M", CatalogURL: "https://m.example/c", PublicKey: repeat('k', 44),
		KeyFingerprint: repeat('f', 16), CreatedAt: now, UpdatedAt: now,
	}
	activation := &model.ThemeActivation{ID: "a", PackageID: "p1", ActorID: "u", ActivatedAt: now}
	for name, call := range map[string]func() error{
		"create package":     func() error { return store.CreatePackage(ctx, pkg) },
		"get package":        func() error { _, err := store.GetPackageByID(ctx, "p1"); return err },
		"get by digest":      func() error { _, err := store.GetPackageByDigest(ctx, "d"); return err },
		"list packages":      func() error { _, err := store.ListPackages(ctx); return err },
		"update state":       func() error { return store.UpdatePackageState(ctx, "p1", model.ThemeStateInstalled, "", now) },
		"activate":           func() error { return store.ActivatePackage(ctx, activation, now) },
		"current activation": func() error { _, err := store.CurrentActivation(ctx); return err },
		"mark unhealthy":     func() error { return store.MarkActivationUnhealthy(ctx, "x", now) },
		"create market":      func() error { return store.CreateMarketSource(ctx, source) },
		"get market":         func() error { _, err := store.GetMarketSource(ctx, "m1"); return err },
		"list markets":       func() error { _, err := store.ListMarketSources(ctx); return err },
		"update market":      func() error { return store.UpdateMarketSource(ctx, source) },
		"delete market":      func() error { return store.DeleteMarketSource(ctx, "m1") },
	} {
		if err := call(); err == nil {
			t.Fatalf("%s on closed database must fail", name)
		}
	}
}
