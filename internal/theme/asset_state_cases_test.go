package theme

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func rewriteSandboxManifest(t *testing.T, raw []byte, change func(*model.ThemeManifest)) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string)
	var manifest model.ThemeManifest
	for _, file := range reader.File {
		content, err := readZipEntry(file, maxAssetFileBytes)
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == manifestName {
			if err := json.Unmarshal(content, &manifest); err != nil {
				t.Fatal(err)
			}
		} else {
			files[file.Name] = string(content)
		}
	}
	change(&manifest)
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return buildThemeZip(t, string(content), files)
}

func TestManifestMetadataAndInventoryErrors(t *testing.T) {
	for name, change := range map[string]func(*model.ThemeManifest){
		"name": func(m *model.ThemeManifest) { m.Name = "" }, "runtime": func(m *model.ThemeManifest) { m.Runtime = "unknown" },
		"version": func(m *model.ThemeManifest) { m.Version = "invalid" }, "maximum": func(m *model.ThemeManifest) { m.MaximumBeatVersion = "bad" },
		"preview path":      func(m *model.ThemeManifest) { m.Preview = "../outside.png" },
		"missing inventory": func(m *model.ThemeManifest) { m.Files[0].Path = "unlisted.js" },
		"size":              func(m *model.ThemeManifest) { m.Files[0].Size++ }, "hash": func(m *model.ThemeManifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) },
		"declarative code": func(m *model.ThemeManifest) { m.Runtime = model.ThemeRuntimeDeclarative },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateArchive(rewriteSandboxManifest(t, sandboxArchive(t, nil), change)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestArchiveCRCAndImageDimensions(t *testing.T) {
	raw := sandboxArchive(t, nil)
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != "assets/main.js" {
			continue
		}
		offset, err := file.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		raw[offset] ^= 0xff
	}
	if _, err := ValidateArchive(raw); err == nil {
		t.Fatal("corrupt compressed content accepted")
	}
	png := []byte(fixturePNG)
	binary.BigEndian.PutUint32(png[16:20], 8192)
	binary.BigEndian.PutUint32(png[20:24], 8192)
	binary.BigEndian.PutUint32(png[29:33], crc32.ChecksumIEEE(png[12:29]))
	if err := validateImage("image/png", png); err == nil {
		t.Fatal("oversized decoded raster accepted")
	}
	for _, kind := range []string{"VP8X", "VP8 "} {
		data := testWebP(kind)[:25]
		binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
		if err := validateWebP(data); err == nil {
			t.Fatalf("short %s accepted", kind)
		}
	}
	data := testWebP("VP8 ")
	data[23] = 0
	if err := validateWebP(data); err == nil {
		t.Fatal("invalid lossy WebP accepted")
	}
}

func TestAssetInvalidStoredMetadata(t *testing.T) {
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service, err := NewService(store.NewThemeStore(database.DB), store.NewAdminStore(database.DB), filepath.Join(t.TempDir(), "themes"), "0.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := service.Install(context.Background(), sandboxArchive(t, nil), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(context.Background(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	descriptor, err := service.ActiveDescriptor(context.Background())
	if err != nil || descriptor.Protocol != 2 || descriptor.SandboxEntry != "index.html" {
		t.Fatalf("descriptor: %+v, %v", descriptor, err)
	}
	for _, digest := range []string{"", "bad", strings.Repeat("g", 64)} {
		if _, err := service.ReadAsset(context.Background(), digest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
			t.Fatal(err)
		}
	}
	if _, err := database.DB.Exec("UPDATE theme_packages SET manifest_json = ? WHERE id = ?", "{", pkg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal("corrupt stored manifest was accepted")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "preview.png"); err == nil {
		t.Fatal("closed metadata store was ignored")
	}
}

func TestAssetFilesystemEntryChangesAreRejected(t *testing.T) {
	service, pkg := assetFixture(t)
	assets := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content", "assets")
	if err := os.Rename(assets, assets+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assets, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "assets/main.js"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal("changed parent directory was accepted")
	}
	if err := os.Rename(service.root, service.root+".offline"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service.root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal("unavailable package root was ignored")
	}
}

func TestSandboxDescriptorRejectsMissingOrAmbiguousEntry(t *testing.T) {
	for _, raw := range []string{"{", `{"runtime":"sandboxed","files":[]}`, `{"runtime":"sandboxed","files":[{"path":"one.html"},{"path":"two.html"}]}`} {
		if entry := sandboxEntryFor(&model.ThemePackage{Runtime: model.ThemeRuntimeSandboxed, ManifestJSON: raw}); entry != "" {
			t.Fatal("invalid entry was exposed")
		}
	}
	service, pkg := assetFixture(t)
	if _, err := service.Content(pkg.ArchiveDigest); err != nil {
		t.Fatal(err)
	}
	raw := rewriteSandboxManifest(t, sandboxArchive(t, nil), func(m *model.ThemeManifest) { m.Version = "1.0.1" })
	staging := filepath.Join(service.root, "staging")
	if err := os.Remove(staging); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staging, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(context.Background(), raw, model.ThemeSourceUpload, "", "owner"); err == nil {
		t.Fatal("unavailable staging directory was ignored")
	}
}
