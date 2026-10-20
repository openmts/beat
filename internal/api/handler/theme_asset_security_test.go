package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

func sandboxHTTPArchive(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{"preview.png": themeFixturePNG, "dist/index.html": `<script type="module" src="main.js"></script>`, "dist/main.js": `export const ready = true`}
	manifest := model.ThemeManifest{SchemaVersion: 1, ID: "asset-test", Name: "Asset test", Description: "Assets", Version: "1.0.0", Author: "Beat", License: "MIT", Runtime: model.ThemeRuntimeSandboxed, MinimumBeatVersion: "0.1.0", Preview: "preview.png"}
	for name, content := range files {
		digest := sha256.Sum256([]byte(content))
		manifest.Files = append(manifest.Files, model.ThemeManifestFile{Path: name, Size: int64(len(content)), SHA256: fmt.Sprintf("%x", digest)})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["beat-theme.json"] = string(raw)
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestThemeHTMLResponseRetainsIsolationOutsideIframe(t *testing.T) {
	handler, service, _ := setupThemeHandler(t)
	pkg, err := service.Install(context.Background(), sandboxHTTPArchive(t), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/themes/assets/"+pkg.ArchiveDigest+"/dist/index.html", nil)
	request.SetPathValue("digest", pkg.ArchiveDigest)
	request.SetPathValue("asset", "dist/index.html")
	response := httptest.NewRecorder()
	middleware.SecurityHeaders(http.HandlerFunc(handler.HandleAsset)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status: %d", response.Code)
	}
	for _, policy := range []string{"sandbox allow-scripts", "frame-ancestors 'self'", "connect-src 'none'"} {
		if !strings.Contains(response.Header().Get("Content-Security-Policy"), policy) {
			t.Fatalf("missing CSP %s", policy)
		}
	}
	if response.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatal("global DENY was not scoped for the sandbox HTML")
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "*" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("opaque module CORS must be public and credentialless")
	}
	if response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("HTML must revalidate")
	}
}

func TestThemeAssetETagNeverBypassesIntegrity(t *testing.T) {
	handler, service, _ := setupThemeHandler(t)
	pkg, err := service.Install(context.Background(), sandboxHTTPArchive(t), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/themes/assets/file", nil)
	request.SetPathValue("digest", pkg.ArchiveDigest)
	request.SetPathValue("asset", "dist/main.js")
	request.Header.Set("Origin", "null")
	response := httptest.NewRecorder()
	handler.HandleAsset(response, request)
	if response.Header().Get("Content-Type") != "application/javascript" {
		t.Fatal("incorrect module MIME")
	}
	request.Header.Set("If-None-Match", response.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	handler.HandleAsset(cached, request)
	if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
		t.Fatal("valid immutable asset was not conditionally cached")
	}
	file := filepath.Join(service.PackageDir(pkg.ArchiveDigest), "content", "dist", "main.js")
	if err := os.WriteFile(file, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := httptest.NewRecorder()
	handler.HandleAsset(changed, request)
	if changed.Code != http.StatusNotFound {
		t.Fatal("ETag bypassed asset integrity checks")
	}
}
