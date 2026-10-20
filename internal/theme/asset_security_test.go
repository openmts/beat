package theme

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func sandboxArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var preview bytes.Buffer
	if err := png.Encode(&preview, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if files == nil {
		files = map[string][]byte{"index.html": []byte(`<script type="module" src="assets/main.js"></script>`), "assets/main.js": []byte(`export const value = 1`)}
	}
	if _, exists := files["preview.png"]; !exists {
		files["preview.png"] = preview.Bytes()
	}
	manifest := model.ThemeManifest{SchemaVersion: 1, ID: "sandbox-test", Name: "Sandbox", Description: "Test bundle", Version: "1.0.0", Author: "Beat", License: "MIT", Runtime: model.ThemeRuntimeSandboxed, MinimumBeatVersion: "0.1.0", Preview: "preview.png"}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		manifest.Files = append(manifest.Files, model.ThemeManifestFile{Path: path, Size: int64(len(files[path])), SHA256: sha256Hex(files[path])})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	files["beat-theme.json"] = raw
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func assetFixture(t *testing.T) (*Service, *model.ThemePackage) {
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
	pkg, err := service.Install(context.Background(), sandboxArchive(t, nil), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	return service, pkg
}

func TestAssetReadRequiresManifestAndIntegrity(t *testing.T) {
	service, pkg := assetFixture(t)
	asset, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "assets/main.js")
	if err != nil || asset.MIME != "application/javascript" || string(asset.Content) != "export const value = 1" {
		t.Fatalf("unexpected asset: %+v, %v", asset, err)
	}
	root := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content")
	if err := os.WriteFile(filepath.Join(root, "unlisted.json"), []byte(`{"private":"never serve"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "unlisted.json"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("unlisted asset error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets/main.js"), []byte("modified script"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "assets/main.js"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("modified asset error: %v", err)
	}
}

func TestAssetReadRejectsPathsSymlinksAndInactivePackages(t *testing.T) {
	service, pkg := assetFixture(t)
	for _, path := range []string{"../archive.zip", "/assets/main.js", "assets/../assets/main.js", "assets\\main.js", "missing.js", "beat-theme.json"} {
		if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, path); !errors.Is(err, ErrContentUnavailable) {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	root := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content")
	original := filepath.Join(root, "assets/main.js")
	if err := os.Rename(original, original+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original+".original", original); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "assets/main.js"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("symlink error: %v", err)
	}
	if err := service.Delete(context.Background(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("quarantined package error: %v", err)
	}
}

func TestSandboxArchiveRequiresOneHTMLAndPrivateResources(t *testing.T) {
	cases := map[string]map[string][]byte{
		"missing entry":    {"main.js": []byte("export default 1")},
		"multiple entries": {"index.html": []byte("<p>one</p>"), "other.html": []byte("<p>two</p>")},
		"remote script":    {"index.html": []byte(`<script src="https://outside.example/main.js"></script>`)},
		"missing script":   {"index.html": []byte(`<script src="missing.js"></script>`)},
		"inline script":    {"index.html": []byte(`<script>alert(1)</script>`)},
		"inline handler":   {"index.html": []byte(`<img src="preview.png" onerror="alert(1)">`)},
		"base":             {"index.html": []byte(`<base href="https://outside.example/"><p>one</p>`)},
		"refresh":          {"index.html": []byte(`<meta http-equiv="refresh" content="0;url=https://outside.example/">`)},
		"fake image":       {"index.html": []byte("<p>one</p>"), "preview.png": []byte("not a PNG")},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateArchive(sandboxArchive(t, files)); err == nil {
				t.Fatal("unsafe sandbox archive accepted")
			}
		})
	}
}
