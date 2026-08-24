package theme

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func buildThemeZip(t *testing.T, manifest string, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	if manifest != "" {
		entry, err := writer.Create("beat-theme.json")
		if err != nil {
			t.Fatalf("create manifest entry: %v", err)
		}
		_, _ = entry.Write([]byte(manifest))
	}
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry %s: %v", name, err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func sampleManifest(t *testing.T, overrides map[string]any) string {
	t.Helper()
	preview := "preview.png"
	files := []model.ThemeManifestFile{
		{Path: "preview.png", Size: 1, SHA256: sha256Hex([]byte("p"))},
		{Path: "assets/theme.css", Size: 1, SHA256: sha256Hex([]byte("c"))},
	}
	base := map[string]any{
		"schema_version": 1, "id": "acme-theme", "name": "Acme",
		"description": "A theme", "version": "1.0.0", "author": "a", "license": "MIT",
		"runtime": "declarative", "minimum_beat_version": "0.1.0",
		"preview": preview, "files": files,
	}
	for key, value := range overrides {
		base[key] = value
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return string(raw)
}

func validPackageBytes(t *testing.T) []byte {
	t.Helper()
	return buildThemeZip(t, sampleManifest(t, nil), map[string]string{
		"preview.png":      "p",
		"assets/theme.css": "c",
	})
}

func TestValidateArchiveRoundTrip(t *testing.T) {
	raw := validPackageBytes(t)
	validated, err := ValidateArchive(raw)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validated.Digest == "" || validated.Manifest.ID != "acme-theme" || len(validated.Files) != 2 {
		t.Fatalf("validated = %#v", validated)
	}
	if validated.Content["assets/theme.css"] == nil || validated.Content["preview.png"] == nil {
		t.Fatalf("content not extracted")
	}
	// 再次校验应稳定（同一字节的 digest）
	again, err := ValidateArchive(raw)
	if err != nil || again.Digest != validated.Digest {
		t.Fatalf("digest unstable: %v / %v", again.Digest, validated.Digest)
	}
}

func TestValidateArchiveRejectsBadInputs(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		raw := buildThemeZip(t, "", map[string]string{"preview.png": "p"})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("missing manifest accepted")
		}
	})
	t.Run("unknown manifest field", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, map[string]any{"evil": 1}), map[string]string{
			"preview.png": "p", "assets/theme.css": "c",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("unknown manifest field accepted")
		}
	})
	t.Run("traversal path", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"../../etc/passwd": "x", "preview.png": "p", "assets/theme.css": "c",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("traversal accepted")
		}
	})
	t.Run("absolute path", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"/etc/passwd": "x", "preview.png": "p", "assets/theme.css": "c",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("absolute path accepted")
		}
	})
	t.Run("duplicate path", func(t *testing.T) {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		manifest := sampleManifest(t, nil)
		for _, name := range []string{"beat-theme.json", "preview.png", "assets/theme.css", "assets/theme.css"} {
			content := manifest
			if name != "beat-theme.json" {
				content = "c"
			}
			entry, createErr := writer.Create(name)
			if createErr != nil {
				t.Fatalf("create %s: %v", name, createErr)
			}
			_, _ = entry.Write([]byte(content))
		}
		_ = writer.Close()
		if _, err := ValidateArchive(buffer.Bytes()); err == nil {
			t.Fatal("duplicate path accepted")
		}
	})
	t.Run("bad version", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, map[string]any{"version": "not-semver"}), map[string]string{
			"preview.png": "p", "assets/theme.css": "c",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("bad version accepted")
		}
	})
	t.Run("manifest size overflow", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, map[string]any{
			"description": strings.Repeat("x", maxManifestBytes),
		}), map[string]string{"preview.png": "p", "assets/theme.css": "c"})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("oversized manifest accepted")
		}
	})
	t.Run("unsupported extension", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", "evil.exe": "MZ",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("exe accepted")
		}
	})
	t.Run("empty archive", func(t *testing.T) {
		if _, err := ValidateArchive([]byte{}); err == nil {
			t.Fatal("empty archive accepted")
		}
	})
}

func TestInstallActivateLifecycle(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	adminStore := store.NewAdminStore(sqlite.DB)
	service, err := NewService(store.NewThemeStore(sqlite.DB), adminStore,
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	ctx := context.Background()

	raw := validPackageBytes(t)
	pkg, err := service.Install(ctx, raw, model.ThemeSourceUpload, "", "owner-1")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if pkg.State != model.ThemeStateInstalled {
		t.Fatalf("state = %s", pkg.State)
	}
	// 相同字节再次安装冲突
	if _, err := service.Install(ctx, raw, model.ThemeSourceUpload, "", "owner-1"); err == nil {
		t.Fatal("duplicate install accepted")
	}
	// 兼容性不符
	rawIncompatible := buildThemeZip(t, sampleManifest(t, map[string]any{"minimum_beat_version": "99.0.0"}), map[string]string{
		"preview.png": "p", "assets/theme.css": "c",
	})
	if _, err := service.Install(ctx, rawIncompatible, model.ThemeSourceUpload, "", "owner-1"); !errors.Is(err, ErrRuntimeIncompatible) {
		t.Fatalf("incompatible install error = %v", err)
	}

	// 激活前是内置主题
	descriptor, err := service.ActiveDescriptor(ctx)
	if err != nil || !descriptor.BuiltIn {
		t.Fatalf("descriptor before activate = %#v / %v", descriptor, err)
	}
	// 兼容性不满足时的安装已失败；激活未激活包应报错
	activated, err := service.Activate(ctx, pkg.ID, "owner-1")
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if activated.ThemeID != "acme-theme" {
		t.Fatalf("activated = %#v", activated)
	}
	descriptor, _ = service.ActiveDescriptor(ctx)
	if descriptor.BuiltIn || descriptor.ThemeID != "acme-theme" {
		t.Fatalf("descriptor after activate = %#v", descriptor)
	}

	// 活动包不可删除
	if err := service.Delete(ctx, pkg.ID, "owner-1"); !errors.Is(err, ErrActivePackage) {
		t.Fatalf("delete active = %v", err)
	}

	// 导出返回原始字节
	exported, filename, err := service.Export(ctx, pkg.ID)
	if err != nil || !bytes.Equal(exported, raw) || !strings.HasSuffix(filename, ".theme.zip") {
		t.Fatalf("export = %d bytes / %s / %v", len(exported), filename, err)
	}

	// 回退到内置
	if _, err := service.Rollback(ctx, "owner-1"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	descriptor, _ = service.ActiveDescriptor(ctx)
	if !descriptor.BuiltIn {
		t.Fatalf("descriptor after rollback = %#v", descriptor)
	}

	// 删除（非活动）
	if err := service.Delete(ctx, pkg.ID, "owner-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// 再次激活已删除包失败
	if _, err := service.Activate(ctx, pkg.ID, "owner-1"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("activate deleted = %v", err)
	}
}

func TestInstallAtomicLeavesNoPartialState(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	// 无效归档：安装失败且不产生任何包记录
	if _, err := service.Install(context.Background(), []byte("not-a-zip"),
		model.ThemeSourceUpload, "", "owner-1"); err == nil {
		t.Fatal("invalid install accepted")
	}
	packages, err := service.ListPackages(context.Background())
	if err != nil || len(packages) != 0 {
		t.Fatalf("packages after failed install = %v / %v", packages, err)
	}
}

func TestMarketFetchValidatesSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	catalog := model.ThemeCatalog{
		SchemaVersion: 1, SourceID: "m1",
		Themes: []model.ThemeCatalogEntry{{ThemeID: "acme", Name: "Acme", Version: "1.0.0"}},
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	signature := ed25519.Sign(privateKey, body)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(catalogSignatureHeader, base64.StdEncoding.EncodeToString(signature))
		_, _ = w.Write(body)
	}))
	defer server.Close()

	// 生产策略拒绝 loopback，测试策略放行
	client := NewMarketClient(true, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.SetRootCAs(pool)
	source := &model.ThemeMarketSource{
		ID: "m1", CatalogURL: server.URL, PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}
	fetched, digest, err := client.FetchCatalog(context.Background(), source)
	if err != nil {
		t.Fatalf("fetch catalog: %v", err)
	}
	if fetched.SourceID != "m1" || len(fetched.Themes) != 1 {
		t.Fatalf("catalog = %#v", fetched)
	}
	expectedDigest := sha256.Sum256(body)
	if digest != hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("digest = %s", digest)
	}
}

func TestMarketRejectsTamperedSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	body := []byte(`{"schema_version":1,"source_id":"m1","themes":[]}`)
	signature := ed25519.Sign(privateKey, body)
	signature[0] ^= 0x01
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(catalogSignatureHeader, base64.StdEncoding.EncodeToString(signature))
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client := NewMarketClient(true, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.SetRootCAs(pool)
	source := &model.ThemeMarketSource{
		ID: "m1", CatalogURL: server.URL, PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}
	if _, _, err := client.FetchCatalog(context.Background(), source); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("tampered signature error = %v", err)
	}
}

func TestMarketRejectsNonPublicURL(t *testing.T) {
	client := NewMarketClient(false, time.Now)
	source := &model.ThemeMarketSource{ID: "m1", CatalogURL: "http://127.0.0.1:8080/catalog"}
	if _, _, err := client.FetchCatalog(context.Background(), source); err == nil {
		t.Fatal("private market url accepted in production policy")
	}
}

func TestDownloadPackage(t *testing.T) {
	content := []byte("package-bytes")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()
	client := NewMarketClient(true, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.SetRootCAs(pool)
	raw, digest, err := client.DownloadPackage(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if !bytes.Equal(raw, content) {
		t.Fatalf("downloaded = %d bytes", len(raw))
	}
	expected := sha256.Sum256(content)
	if digest != hex.EncodeToString(expected[:]) {
		t.Fatalf("digest = %s", digest)
	}
}

func TestValidSemver(t *testing.T) {
	for _, version := range []string{"1.0.0", "0.1.0", "2.3.4-rc.1", "1.2.3+build"} {
		if !validSemver(version) {
			t.Fatalf("valid semver %q rejected", version)
		}
	}
	for _, version := range []string{"", "1.0", "1.0.0.1", "one.two.three", "1.0.x"} {
		if validSemver(version) {
			t.Fatalf("invalid semver %q accepted", version)
		}
	}
}

func TestPermissionsAndDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "themes")
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	if _, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB), root, "0.1.0", time.Now); err != nil {
		t.Fatalf("new service: %v", err)
	}
	for _, sub := range []string{"packages", "staging", "quarantine"} {
		info, err := os.Stat(filepath.Join(root, sub))
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s permissions = %v / %v", sub, info.Mode().Perm(), err)
		}
	}
}

func TestThemeServiceErrorPathsWithClosedDatabase(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	adminStore := store.NewAdminStore(sqlite.DB)
	service, err := NewService(store.NewThemeStore(sqlite.DB), adminStore,
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	markets := NewMarketService(store.NewThemeStore(sqlite.DB), NewMarketClient(true, time.Now), service, time.Now)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	ctx := context.Background()
	raw := validPackageBytes(t)
	if _, err := service.Install(ctx, raw, model.ThemeSourceUpload, "", "u"); err == nil {
		t.Fatal("install on closed db must fail")
	}
	if _, err := service.Activate(ctx, "p1", "u"); err == nil {
		t.Fatal("activate on closed db must fail")
	}
	if _, err := service.Rollback(ctx, "u"); err == nil {
		t.Fatal("rollback on closed db must fail")
	}
	if err := service.Fallback(ctx, "u", "x"); err == nil {
		t.Fatal("fallback on closed db must fail")
	}
	if err := service.MarkUnhealthy(ctx, "x"); err == nil {
		t.Fatal("mark unhealthy on closed db must fail")
	}
	if _, _, err := service.Export(ctx, "p1"); err == nil {
		t.Fatal("export on closed db must fail")
	}
	if err := service.Delete(ctx, "p1", "u"); err == nil {
		t.Fatal("delete on closed db must fail")
	}
	if _, err := service.ActiveDescriptor(ctx); err == nil {
		t.Fatal("active descriptor on closed db must fail")
	}
	if _, err := service.ListPackages(ctx); err == nil {
		t.Fatal("list on closed db must fail")
	}
	if _, err := markets.ListSources(ctx); err == nil {
		t.Fatal("list markets on closed db must fail")
	}
	if _, err := markets.CreateSource(ctx, &model.ThemeMarketSource{
		Name: "M", CatalogURL: "https://m.example/c", PublicKey: repeat('k', 44),
	}); err == nil {
		t.Fatal("create market on closed db must fail")
	}
	if _, err := markets.UpdateSource(ctx, "m1", model.ThemeMarketSource{Name: "N"}); err == nil {
		t.Fatal("update market on closed db must fail")
	}
	if _, err := markets.RefreshCatalog(ctx, "m1"); err == nil {
		t.Fatal("refresh catalog on closed db must fail")
	}
	if _, err := markets.InstallFromMarket(ctx, "m1", "t", "u"); err == nil {
		t.Fatal("market install on closed db must fail")
	}
}

func TestMarketServiceValidation(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	markets := NewMarketService(store.NewThemeStore(sqlite.DB), NewMarketClient(true, time.Now), service, time.Now)
	// 缺名称/URL/密钥
	if _, err := markets.CreateSource(context.Background(), &model.ThemeMarketSource{
		Name: "M",
	}); err == nil {
		t.Fatal("incomplete market source accepted")
	}
	// 更新不存在的源
	if _, err := markets.UpdateSource(context.Background(), "missing", model.ThemeMarketSource{
		Name: "N", CatalogURL: "https://m.example/c", PublicKey: repeat('k', 44),
	}); !errors.Is(err, store.ErrThemeMarketNotFound) {
		t.Fatalf("update missing = %v", err)
	}
	// 刷新不存在的源
	if _, err := markets.RefreshCatalog(context.Background(), "missing"); !errors.Is(err, store.ErrThemeMarketNotFound) {
		t.Fatalf("refresh missing = %v", err)
	}
	// 删除不存在的源
	if err := markets.DeleteSource(context.Background(), "missing"); !errors.Is(err, store.ErrThemeMarketNotFound) {
		t.Fatalf("delete missing = %v", err)
	}
}

func repeat(character rune, count int) string {
	out := make([]byte, count)
	for index := range out {
		out[index] = byte(character)
	}
	return string(out)
}

func TestMarketServiceRefreshAndInstall(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	packageBytes := validPackageBytes(t)
	packageDigest := fmt.Sprintf("%x", sha256.Sum256(packageBytes))

	packageServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(packageBytes)
	}))
	defer packageServer.Close()

	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	themeService, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	client := NewMarketClient(true, time.Now)
	markets := NewMarketService(store.NewThemeStore(sqlite.DB), client, themeService, time.Now)

	created, err := markets.CreateSource(context.Background(), &model.ThemeMarketSource{
		Name: "Market", CatalogURL: "https://placeholder.invalid/catalog",
		PublicKey:      base64.StdEncoding.EncodeToString(publicKey),
		KeyFingerprint: "fp", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create market: %v", err)
	}

	catalog := model.ThemeCatalog{
		SchemaVersion: 1, SourceID: created.ID,
		Themes: []model.ThemeCatalogEntry{{
			ThemeID: "acme-theme", Name: "Acme", Version: "1.0.0", Author: "a",
			Runtime: model.ThemeRuntimeDeclarative, PackageURL: packageServer.URL,
			PackageSHA256: packageDigest, MinimumBeat: "0.1.0",
		}},
	}
	body, _ := json.Marshal(catalog)
	signature := ed25519.Sign(privateKey, body)
	catalogServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(catalogSignatureHeader, base64.StdEncoding.EncodeToString(signature))
		_, _ = w.Write(body)
	}))
	defer catalogServer.Close()

	pool := x509.NewCertPool()
	pool.AddCert(catalogServer.Certificate())
	pool.AddCert(packageServer.Certificate())
	client.SetRootCAs(pool)

	created.CatalogURL = catalogServer.URL
	if _, err := markets.UpdateSource(context.Background(), created.ID, *created); err != nil {
		t.Fatalf("update market url: %v", err)
	}

	refreshed, err := markets.RefreshCatalog(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("refresh catalog: %v", err)
	}
	if refreshed == nil || len(refreshed.Themes) != 1 {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	cached, err := markets.RefreshCatalog(context.Background(), created.ID)
	if err != nil || cached != nil {
		t.Fatalf("cached refresh = %v / %v", cached, err)
	}

	pkg, err := markets.InstallFromMarket(context.Background(), created.ID, "acme-theme", "owner-1")
	if err != nil {
		t.Fatalf("market install: %v", err)
	}
	if pkg.ThemeID != "acme-theme" || pkg.SourceType != model.ThemeSourceMarket {
		t.Fatalf("installed = %#v", pkg)
	}
	if _, err := markets.InstallFromMarket(context.Background(), created.ID, "missing", "owner-1"); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("unknown theme error = %v", err)
	}
}

func TestValidateArchiveRejectsMoreInputs(t *testing.T) {
	t.Run("oversized archive", func(t *testing.T) {
		if _, err := ValidateArchive(make([]byte, maxArchiveBytes+1)); err == nil {
			t.Fatal("oversized archive accepted")
		}
	})
	t.Run("compression bomb", func(t *testing.T) {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		entry, _ := writer.Create("beat-theme.json")
		_, _ = entry.Write([]byte(sampleManifest(t, nil)))
		big, _ := writer.Create("preview.png")
		_, _ = big.Write(bytes.Repeat([]byte{0}, 1<<20))
		_ = writer.Close()
		if _, err := ValidateArchive(buffer.Bytes()); err == nil {
			t.Fatal("compression bomb accepted")
		}
	})
	t.Run("backslash path", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", `evil\file.css`: "x",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("backslash path accepted")
		}
	})
	t.Run("dot segment", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", "assets/./x.css": "x",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("dot segment accepted")
		}
	})
	t.Run("nested manifest", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", "sub/beat-theme.json": "x",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("nested manifest accepted")
		}
	})
	t.Run("preview missing", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{"assets/theme.css": "c"})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("missing preview accepted")
		}
	})
}

func TestServiceExportAndActiveEdgeCases(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	// 导出不存在的包
	if _, _, err := service.Export(ctx, "missing"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("export missing = %v", err)
	}
	// 删除不存在的包
	if err := service.Delete(ctx, "missing", "u"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("delete missing = %v", err)
	}
	// 无激活时回退（内置）与激活缺省包
	if _, err := service.Rollback(ctx, "u"); err != nil {
		t.Fatalf("rollback no activation: %v", err)
	}
	// 内容目录缺失时激活失败（无安装记录）
	if _, err := service.Activate(ctx, "p1", "u"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("activate missing = %v", err)
	}
}

func TestValidateArchiveAdditionalLimits(t *testing.T) {
	t.Run("empty manifest only", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), nil)
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("archive without declared files but preview missing accepted")
		}
	})
	t.Run("too many files", func(t *testing.T) {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		entry, _ := writer.Create("beat-theme.json")
		_, _ = entry.Write([]byte(sampleManifest(t, nil)))
		for index := 0; index <= maxFiles; index++ {
			file, _ := writer.Create(fmt.Sprintf("f%04d.css", index))
			_, _ = file.Write([]byte("x"))
		}
		_ = writer.Close()
		if _, err := ValidateArchive(buffer.Bytes()); err == nil {
			t.Fatal("too many files accepted")
		}
	})
	t.Run("oversized file", func(t *testing.T) {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		entry, _ := writer.Create("beat-theme.json")
		_, _ = entry.Write([]byte(sampleManifest(t, nil)))
		big, _ := writer.Create("preview.png")
		_, _ = big.Write(make([]byte, maxAssetFileBytes+1))
		_ = writer.Close()
		if _, err := ValidateArchive(buffer.Bytes()); err == nil {
			t.Fatal("oversized file accepted")
		}
	})
	t.Run("nul character path", func(t *testing.T) {
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", "bad\x00.css": "x",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("nul path accepted")
		}
	})
	t.Run("path too long", func(t *testing.T) {
		long := strings.Repeat("a", maxPathBytes+1)
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", long + ".css": "x",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("long path accepted")
		}
	})
	t.Run("too deep", func(t *testing.T) {
		deep := strings.Join([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, "/") + ".css"
		raw := buildThemeZip(t, sampleManifest(t, nil), map[string]string{
			"preview.png": "p", "assets/theme.css": "c", deep: "x",
		})
		if _, err := ValidateArchive(raw); err == nil {
			t.Fatal("too deep path accepted")
		}
	})
}

func TestThemeHelperFunctions(t *testing.T) {
	if !isIDCharacter('a') || !isIDCharacter('0') || !isIDCharacter('-') || isIDCharacter('A') {
		t.Fatal("isIDCharacter misbehaves")
	}
	if !validThemeID("abc-def_1") || validThemeID("AB") || validThemeID("a b") {
		t.Fatal("validThemeID misbehaves")
	}
}

func TestMarketClientErrorPaths(t *testing.T) {
	t.Run("non-ok status", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()
		client := NewMarketClient(true, time.Now)
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		client.SetRootCAs(pool)
		source := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		if _, _, err := client.FetchCatalog(context.Background(), source); !errors.Is(err, ErrMarketUnavailable) {
			t.Fatalf("non-ok fetch = %v", err)
		}
	})
	t.Run("oversized catalog", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(make([]byte, maxCatalogBytes+1))
		}))
		defer server.Close()
		client := NewMarketClient(true, time.Now)
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		client.SetRootCAs(pool)
		source := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		if _, _, err := client.FetchCatalog(context.Background(), source); !errors.Is(err, ErrMarketInvalid) {
			t.Fatalf("oversized fetch = %v", err)
		}
	})
	t.Run("bad signature format", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(catalogSignatureHeader, "not-base64")
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()
		client := NewMarketClient(true, time.Now)
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		client.SetRootCAs(pool)
		source := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		if _, _, err := client.FetchCatalog(context.Background(), source); !errors.Is(err, ErrMarketInvalid) {
			t.Fatalf("bad signature = %v", err)
		}
	})
	t.Run("download non-ok", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}))
		defer server.Close()
		client := NewMarketClient(true, time.Now)
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		client.SetRootCAs(pool)
		if _, _, err := client.DownloadPackage(context.Background(), server.URL); !errors.Is(err, ErrMarketUnavailable) {
			t.Fatalf("download non-ok = %v", err)
		}
	})
	t.Run("download bad url", func(t *testing.T) {
		client := NewMarketClient(false, time.Now)
		if _, _, err := client.DownloadPackage(context.Background(), "http://127.0.0.1/x"); err == nil {
			t.Fatal("bad download url accepted")
		}
	})
	t.Run("redirect too many", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, server.URL, http.StatusFound)
		}))
		defer server.Close()
		client := NewMarketClient(true, time.Now)
		pool := x509.NewCertPool()
		pool.AddCert(server.Certificate())
		client.SetRootCAs(pool)
		source := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		if _, _, err := client.FetchCatalog(context.Background(), source); err == nil {
			t.Fatal("redirect loop accepted")
		}
	})
}

func TestServiceMultiVersionLifecycle(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()

	// 版本 1.0.0
	v1 := buildThemeZip(t, sampleManifest(t, nil), map[string]string{"preview.png": "p", "assets/theme.css": "c"})
	pkg1, err := service.Install(ctx, v1, model.ThemeSourceUpload, "", "u")
	if err != nil {
		t.Fatalf("install v1: %v", err)
	}
	// 版本 1.1.0（同 theme_id，不同 version）
	v2 := buildThemeZip(t, sampleManifest(t, map[string]any{"version": "1.1.0"}), map[string]string{
		"preview.png": "p", "assets/theme.css": "c",
	})
	pkg2, err := service.Install(ctx, v2, model.ThemeSourceUpload, "", "u")
	if err != nil {
		t.Fatalf("install v2: %v", err)
	}

	if _, err := service.Activate(ctx, pkg1.ID, "u"); err != nil {
		t.Fatalf("activate v1: %v", err)
	}
	if _, err := service.Activate(ctx, pkg2.ID, "u"); err != nil {
		t.Fatalf("activate v2: %v", err)
	}
	descriptor, _ := service.ActiveDescriptor(ctx)
	if descriptor.ThemeID != "acme-theme" || descriptor.Version != "1.1.0" {
		t.Fatalf("descriptor = %#v", descriptor)
	}

	// 回滚应回到 v1（前一激活）
	if _, err := service.Rollback(ctx, "u"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	descriptor, _ = service.ActiveDescriptor(ctx)
	if descriptor.Version != "1.0.0" {
		t.Fatalf("descriptor after rollback = %#v", descriptor)
	}

	// 标记不健康
	if err := service.MarkUnhealthy(ctx, "iframe_timeout"); err != nil {
		t.Fatalf("mark unhealthy: %v", err)
	}

	// 重新激活 v2，再删除 v1（非活动）并导出 v1 应失败
	if _, err := service.Activate(ctx, pkg2.ID, "u"); err != nil {
		t.Fatalf("re-activate v2: %v", err)
	}
	if err := service.Delete(ctx, pkg1.ID, "u"); err != nil {
		t.Fatalf("delete v1: %v", err)
	}
	if _, _, err := service.Export(ctx, pkg1.ID); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("export deleted = %v", err)
	}

	// 回退到内置
	if err := service.Fallback(ctx, "u", "test"); err != nil {
		t.Fatalf("fallback: %v", err)
	}
	descriptor, _ = service.ActiveDescriptor(ctx)
	if !descriptor.BuiltIn {
		t.Fatalf("descriptor after fallback = %#v", descriptor)
	}

	// 重新激活 v2 后列出应标记活动
	if _, err := service.Activate(ctx, pkg2.ID, "u"); err != nil {
		t.Fatalf("re-activate v2: %v", err)
	}
	packages, err := service.ListPackages(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	activeFound := false
	for _, pkg := range packages {
		if pkg.IsActive {
			activeFound = true
			if pkg.ID != pkg2.ID || pkg.HealthState != "healthy" {
				t.Fatalf("active package = %#v", pkg)
			}
		}
	}
	if !activeFound {
		t.Fatal("expected an active package in the list")
	}
	// 回退后不再有活动包
	if err := service.Fallback(ctx, "u", "test"); err != nil {
		t.Fatalf("fallback: %v", err)
	}
	packages, _ = service.ListPackages(ctx)
	for _, pkg := range packages {
		if pkg.IsActive {
			t.Fatalf("unexpected active package after fallback: %#v", pkg)
		}
	}
}

func TestMarketServiceBranchCoverage(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	client := NewMarketClient(true, time.Now)
	markets := NewMarketService(store.NewThemeStore(sqlite.DB), client, service, time.Now)
	ctx := context.Background()

	source, err := markets.CreateSource(ctx, &model.ThemeMarketSource{
		Name: "M", CatalogURL: "https://placeholder.invalid/c", PublicKey: repeat('k', 44),
		KeyFingerprint: "fp", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 更新 URL（触发缓存失效分支）
	source.CatalogURL = "https://new.example/c"
	updated, err := markets.UpdateSource(ctx, source.ID, *source)
	if err != nil || updated.CatalogURL != "https://new.example/c" {
		t.Fatalf("update url = %v / %v", updated, err)
	}
	// 更新公钥（触发密钥变更分支）
	source.PublicKey = repeat('z', 44)
	source.KeyFingerprint = "new-fp"
	if _, err := markets.UpdateSource(ctx, source.ID, *source); err != nil {
		t.Fatalf("update key: %v", err)
	}
	// 指向不可达目录：刷新与安装走 fetch 失败分支
	unreachable := &model.ThemeMarketSource{
		ID: "m2", Name: "Unreachable", CatalogURL: "https://unreachable.invalid/catalog",
		PublicKey: repeat('q', 44), KeyFingerprint: "fp2", Enabled: true,
	}
	createdUnreachable, err := markets.CreateSource(ctx, unreachable)
	if err != nil {
		t.Fatalf("create unreachable: %v", err)
	}
	if _, err := markets.RefreshCatalog(ctx, createdUnreachable.ID); !errors.Is(err, ErrMarketUnavailable) {
		t.Fatalf("refresh unreachable = %v", err)
	}
	if _, err := markets.InstallFromMarket(ctx, createdUnreachable.ID, "t", "u"); !errors.Is(err, ErrMarketUnavailable) {
		t.Fatalf("install unreachable = %v", err)
	}
	// 禁用后刷新与安装失败
	source.Enabled = false
	if _, err := markets.UpdateSource(ctx, source.ID, *source); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := markets.RefreshCatalog(ctx, source.ID); !errors.Is(err, ErrMarketUnavailable) {
		t.Fatalf("refresh disabled = %v", err)
	}
	if _, err := markets.InstallFromMarket(ctx, source.ID, "t", "u"); !errors.Is(err, ErrMarketUnavailable) {
		t.Fatalf("install disabled = %v", err)
	}
}

func TestNewServiceErrorPaths(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	// root 指向一个文件 → MkdirAll/Chmod 失败
	badRoot := filepath.Join(t.TempDir(), "themes")
	if err := os.WriteFile(badRoot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if _, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		badRoot, "0.1.0", time.Now); err == nil {
		t.Fatal("service over a file root accepted")
	}
	// 子目录不可创建（父级为只读文件路径）
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if _, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(parent, "themes"), "0.1.0", time.Now); err == nil {
		t.Fatal("service with uncreatable child accepted")
	}
}

func TestServiceContentHelpers(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, err := service.Content(""); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("content empty id = %v", err)
	}
	if service.PackageDir("abc") == "" {
		t.Fatal("package dir must be non-empty")
	}
}

func TestActivateAndDeleteEdgeBranches(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()
	raw := validPackageBytes(t)
	pkg, err := service.Install(ctx, raw, model.ThemeSourceUpload, "", "u")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// 隔离后激活 → ErrContentUnavailable
	if err := service.Delete(ctx, pkg.ID, "u"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := service.Activate(ctx, pkg.ID, "u"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("activate quarantined = %v", err)
	}
	// 二次删除 → 幂等成功（已隔离）
	if err := service.Delete(ctx, pkg.ID, "u"); err != nil {
		t.Fatalf("second delete = %v", err)
	}
	// 兼容性校验（最大版本小于当前）
	rawMax := buildThemeZip(t, sampleManifest(t, map[string]any{"maximum_beat_version": "0.0.1"}), map[string]string{
		"preview.png": "p", "assets/theme.css": "c",
	})
	if _, err := service.Install(ctx, rawMax, model.ThemeSourceUpload, "", "u"); !errors.Is(err, ErrRuntimeIncompatible) {
		t.Fatalf("max version incompatible = %v", err)
	}
}

func TestMarketFetch304AndKeyErrors(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	body := []byte(`{"schema_version":1,"source_id":"m1","themes":[]}`)
	sig := ed25519.Sign(priv, body)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"etag-1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"etag-1"`)
		w.Header().Set(catalogSignatureHeader, base64.StdEncoding.EncodeToString(sig))
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewMarketClient(true, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.SetRootCAs(pool)

	source := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL,
		PublicKey: base64.StdEncoding.EncodeToString(pub), CacheETag: `"etag-1"`}
	catalog, digest, err := client.FetchCatalog(context.Background(), source)
	if err != nil || catalog != nil {
		t.Fatalf("304 fetch = %v / %v / %v", catalog, digest, err)
	}
	// 密钥长度错误 → ErrMarketInvalid
	badKey := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL,
		PublicKey: base64.StdEncoding.EncodeToString([]byte("short"))}
	if _, _, err := client.FetchCatalog(context.Background(), badKey); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("bad key = %v", err)
	}
}

func TestWritePrivateAndMarketBranches(t *testing.T) {
	// writePrivate 的 OpenFile 失败分支
	if err := writePrivate(filepath.Join("/dev/null", "x"), []byte("a")); err == nil {
		t.Fatal("writePrivate under /dev/null accepted")
	}
	// FetchCatalog：目录 source_id 与源不匹配
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	body := []byte(`{"schema_version":1,"source_id":"other","themes":[]}`)
	sig := ed25519.Sign(priv, body)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(catalogSignatureHeader, base64.StdEncoding.EncodeToString(sig))
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewMarketClient(true, time.Now)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.SetRootCAs(pool)
	source := &model.ThemeMarketSource{ID: "m1", CatalogURL: server.URL,
		PublicKey: base64.StdEncoding.EncodeToString(pub)}
	if _, _, err := client.FetchCatalog(context.Background(), source); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("source id mismatch = %v", err)
	}
	// DownloadPackage：超大响应
	bigServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, maxPackageBytes+1))
	}))
	defer bigServer.Close()
	bigClient := NewMarketClient(true, time.Now)
	bigPool := x509.NewCertPool()
	bigPool.AddCert(bigServer.Certificate())
	bigClient.SetRootCAs(bigPool)
	if _, _, err := bigClient.DownloadPackage(context.Background(), bigServer.URL); !errors.Is(err, ErrMarketInvalid) {
		t.Fatalf("oversized download = %v", err)
	}
	// NewMarketClient 默认 now
	_ = NewMarketClient(true, nil)
}
