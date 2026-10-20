package theme

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestAssetContentTypesAndBudgets(t *testing.T) {
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	valid := map[string][]byte{"a.png": []byte(fixturePNG), "a.jpeg": jpegBytes.Bytes(), "a.webp": testWebP("VP8L"), "a.woff2": testFont(), "a.json": []byte(`{"ok":true}`), "a.html": []byte("<p>ok</p>"), "a.css": []byte("body{}"), "a.js": []byte("export default 1")}
	for name, content := range valid {
		if _, err := validateAssetContent(name, content); err != nil {
			t.Fatalf("valid %s: %v", name, err)
		}
	}
	invalid := map[string][]byte{"a.svg": []byte("<svg/>"), "a.png": []byte("not png"), "a.woff2": []byte("font"), "a.json": []byte("{"), "a.js": {0xff}, "a.css": {0}, "a.html": bytes.Repeat([]byte("x"), maxDataFileBytes+1)}
	for name, content := range invalid {
		if _, err := validateAssetContent(name, content); err == nil {
			t.Fatalf("invalid %s accepted", name)
		}
	}
	if _, err := validateAssetContent("a.png", make([]byte, maxAssetFileBytes+1)); err == nil {
		t.Fatal("oversized raster accepted")
	}
	if err := validateImage("image/png", []byte("\x89PNG\r\n\x1a\n")); err == nil {
		t.Fatal("truncated PNG accepted")
	}
}

func testFont() []byte {
	font := make([]byte, 48)
	copy(font, "wOF2")
	binary.BigEndian.PutUint32(font[8:12], uint32(len(font)))
	binary.BigEndian.PutUint16(font[12:14], 1)
	binary.BigEndian.PutUint32(font[16:20], 48)
	return font
}

func testWebP(kind string) []byte {
	data := make([]byte, 30)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 22)
	copy(data[8:12], "WEBP")
	copy(data[12:16], kind)
	switch kind {
	case "VP8L":
		data[20] = 0x2f
	case "VP8 ":
		copy(data[23:26], "\x9d\x01\x2a")
		data[26], data[28] = 1, 1
	}
	return data
}

func TestWebPAndFontExpandedSizeLimits(t *testing.T) {
	for _, kind := range []string{"VP8L", "VP8X", "VP8 "} {
		if err := validateWebP(testWebP(kind)); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	for _, mutate := range []func([]byte){func(data []byte) { data[4]++ }, func(data []byte) { copy(data[12:16], "FAIL") }, func(data []byte) { data[20] = 0 }, func(data []byte) { binary.LittleEndian.PutUint32(data[21:25], 0x0fffffff) }} {
		data := testWebP("VP8L")
		mutate(data)
		if err := validateWebP(data); err == nil {
			t.Fatal("invalid WebP accepted")
		}
	}
	if err := validateWebP(make([]byte, 20)); err == nil {
		t.Fatal("short WebP accepted")
	}
	font := testFont()
	binary.BigEndian.PutUint32(font[16:20], maxExtracted+1)
	if err := validateFont(font); err == nil {
		t.Fatal("oversized expanded font accepted")
	}
}

func TestArchivePathAndManifestStrictness(t *testing.T) {
	for _, path := range []string{"", "../file.js", "nested/.hidden.js", "C:file.js", "a\\b.js", "a//b.js", "/file.js", "\xff.js", "a\x7f.js"} {
		if _, err := validateEntryPath(path); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	if _, err := parseManifest([]byte(sampleManifest(t, nil) + ` {}`)); err == nil {
		t.Fatal("multiple manifests accepted")
	}
	if _, err := parseManifest([]byte(sampleManifest(t, map[string]any{"preview": "assets/theme.css"}))); err == nil {
		t.Fatal("non-raster preview accepted")
	}
	manifest := model.ThemeManifest{Preview: "preview.png", Files: []model.ThemeManifestFile{{Path: "preview.png"}, {Path: "preview.png"}}}
	if err := validateManifestReferencedFiles(manifest, map[string]ManifestFile{"preview.png": {Path: "preview.png"}}); err == nil {
		t.Fatal("duplicate inventory accepted")
	}
	if _, err := inspectArchive(&zip.Reader{File: []*zip.File{{FileHeader: zip.FileHeader{Name: "../directory/"}}}}); err == nil {
		t.Fatal("unsafe directory entry accepted")
	}
}

func TestHTMLResourcesAndUnsupportedCapabilities(t *testing.T) {
	files := map[string][]byte{"dist/index.html": []byte(`<link rel="stylesheet" href="style.css"><script src="main.js"></script><img src="../preview.png"><a href="#section">go</a>`), "dist/style.css": []byte("body{}"), "dist/main.js": []byte("export default 1"), "preview.png": []byte(fixturePNG)}
	if err := validateHTML("dist/index.html", files); err != nil {
		t.Fatal(err)
	}
	for _, html := range []string{`<form></form>`, `<iframe></iframe>`, `<script src="main.js">alert(1)</script>`, `<a href="/admin">go</a>`, `<img srcset="../preview.png 1x">`, `<button formaction="/admin">go</button>`} {
		files["dist/index.html"] = []byte(html)
		if err := validateHTML("dist/index.html", files); err == nil {
			t.Fatalf("unsupported HTML accepted: %s", html)
		}
	}
	for _, reference := range []string{"https://outside.example/main.js", "//outside.example/x", "/admin", "../../outside.js", "missing.js", "#fragment", "bad%zz", `a\b.js`} {
		if err := validateResourceURL("dist/index.html", reference, files); err == nil {
			t.Fatalf("unsafe reference accepted: %s", reference)
		}
	}
	files["dist/index.html"] = []byte(strings.Repeat("<span></span>", maxHTMLNodes))
	if err := validateHTML("dist/index.html", files); err == nil {
		t.Fatal("excessive DOM accepted")
	}
}

func TestAssetReadRejectsCorruptAndMissingPackageState(t *testing.T) {
	service, pkg := assetFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.ReadAsset(ctx, pkg.ArchiveDigest, "preview.png"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), strings.Repeat("f", 64), "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal(err)
	}
	root := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content")
	path := filepath.Join(root, "assets/main.js")
	if err := os.WriteFile(path, []byte("export const value = 2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "assets/main.js"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal("hash mismatch was accepted")
	}
	if err := os.Remove(filepath.Join(root, "preview.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadAsset(context.Background(), pkg.ArchiveDigest, "preview.png"); !errors.Is(err, ErrContentUnavailable) {
		t.Fatal("missing content accepted")
	}
	if _, ok := manifestAsset(model.ThemeManifest{Files: []model.ThemeManifestFile{{Path: "bad", Size: -1, SHA256: "bad"}}}, "bad"); ok {
		t.Fatal("invalid manifest asset accepted")
	}
	var manifest model.ThemeManifest
	if err := json.Unmarshal([]byte(pkg.ManifestJSON), &manifest); err != nil {
		t.Fatal(err)
	}
	if entry, err := sandboxEntry(manifest); err != nil || entry != "index.html" {
		t.Fatalf("entry: %s %v", entry, err)
	}
}
