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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
)

// 显式启用的临时浏览器fixture；默认测试不启动常驻服务。
func TestThemeBrowserFixture(t *testing.T) {
	if os.Getenv("BEAT_THEME_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture not requested")
	}
	handler, service, _ := setupThemeHandler(t)
	archive := themeBrowserArchive(t, "browser-theme")
	pkg, err := service.Install(context.Background(), archive, model.ThemeSourceUpload, "", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Activate(context.Background(), pkg.ID, "owner-1"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /themes/assets/{digest}/{asset...}", handler.HandleAsset)
	mux.HandleFunc("GET /api/v1/themes/active", handler.HandleActive)
	mux.HandleFunc("GET /fixture/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"fixture": "beat-theme-browser-fixture", "entry": "/themes/assets/" + pkg.ArchiveDigest + "/dist/index.html"})
	})
	mux.HandleFunc("GET /api/v1/admin/themes", handler.HandleList)
	mux.HandleFunc("POST /api/v1/admin/themes/upload", func(w http.ResponseWriter, r *http.Request) {
		handler.HandleUpload(w, r.WithContext(themeOwnerContext().Context()))
	})
	uploadArchive := themeBrowserArchive(t, "browser-upload")
	mux.HandleFunc("GET /fixture/upload-archive", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(uploadArchive) })
	var counter atomic.Int64
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) { browserFixtureAPI(w, r, &counter) })
	mux.HandleFunc("POST /fixture/upload", func(w http.ResponseWriter, r *http.Request) {
		handler.HandleUpload(w, r.WithContext(themeOwnerContext().Context()))
	})
	mux.HandleFunc("GET /fixture/archive", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) })
	stop := make(chan struct{})
	var once sync.Once
	mux.HandleFunc("POST /fixture/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		once.Do(func() { close(stop) })
	})
	mux.HandleFunc("/", themeFixtureStatic(t, filepath.Clean("../../../webui/dist")))
	server := httptest.NewServer(middleware.SecurityHeaders(mux))
	t.Cleanup(server.Close)
	_, _ = fmt.Fprintf(os.Stdout, "THEME_FIXTURE=%s\nTHEME_ENTRY=%s/themes/assets/%s/dist/index.html\n", server.URL, server.URL, pkg.ArchiveDigest)
	<-stop
}

func browserFixtureAPI(w http.ResponseWriter, request *http.Request, counter *atomic.Int64) {
	if themeMetricFixtureAPI(w, request) {
		return
	}
	data := any([]any{})
	switch request.URL.Path {
	case "/api/v1/settings/site":
		data = map[string]any{"site_title": "Beat theme fixture", "site_description": "Temporary public data", "logo_url": "", "favicon_url": "", "default_theme": "light", "public_address_mode": "hidden", "show_network_quality": false}
	case "/api/v1/auth/state":
		data = map[string]any{"setup_required": false}
	case "/api/v1/auth/session":
		data = map[string]any{"user": map[string]any{"id": "fixture-owner", "username": "fixture", "display_name": "Fixture owner", "role": "owner", "enabled": true}, "session": map[string]any{"id": "fixture-session", "reauthenticated_until": "2099-01-01T00:00:00Z"}}
	case "/api/v1/settings/maintenance":
		data = map[string]any{"settings": map[string]any{"retention_days": 30, "auto_cleanup_enabled": true, "cleanup_hour_utc": 3}, "status": map[string]any{"running": false, "last_status": "never", "last_error": "", "last_duration_ms": 0}, "storage": map[string]any{"mts_bytes": 0, "sqlite_bytes": 0, "total_bytes": 0, "mts_healthy": true, "mts_health_reasons": []string{}}}
	case "/api/v1/groups":
		data = []any{map[string]any{"id": "group", "name": "Public group", "sort_order": 0}}
	case "/api/v1/nodes":
		data = []any{browserFixtureNode(counter.Add(1)), map[string]any{"id": "private-node", "name": "HIDDEN_NODE", "group_id": "group", "status": "online", "is_public": false}}
	case "/api/v1/nodes/public-node":
		data = browserFixtureNode(counter.Add(1))
	case "/api/v1/nodes/public-node/metrics":
		data = map[string]any{"cpu": []any{map[string]any{"timestamp": time.Now().Unix() - 1, "value": 12}}, "private_metric": []any{map[string]any{"timestamp": time.Now().Unix() - 1, "value": 999}}}
	case "/api/v1/nodes/public-node/custom-metrics":
		data = []any{}
	default:
		if strings.HasPrefix(request.URL.Path, "/api/v1/admin/") {
			JSONError(w, http.StatusForbidden, "fixture admin unavailable")
			return
		}
	}
	JSONResponse(w, http.StatusOK, data)
}

func browserFixtureNode(value int64) map[string]any {
	return map[string]any{"id": "public-node", "name": "Public node", "alias": "Fixture", "group_id": "group", "status": "online", "is_public": true,
		"telemetry_at": time.Now().UTC().Format(time.RFC3339), "telemetry_age_seconds": 0, "telemetry_state": "fresh",
		"host": "IP_MUST_BE_HIDDEN", "ssh_public_key": "PRIVATE_KEY_SENTINEL", "private_remark": "PRIVATE_REMARK_SENTINEL", "agent_token": "TOKEN_SENTINEL",
		"tags": []string{"public"}, "public_remark": "Visible remark", "metrics": map[string]any{"cpu": value, "memory": 40, "private_metric": 999}}
}

func themeBrowserArchive(t *testing.T, themeID string) []byte {
	t.Helper()
	font, err := os.ReadFile("../../../webui/node_modules/@fontsource-variable/geist/files/geist-latin-wght-normal.woff2")
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := os.ReadFile("../../../themes/sdk/bridge.js")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"preview.png": []byte(themeFixturePNG), "dist/font.woff2": font, "dist/beat-sdk.js": sdk,
		"dist/index.html": []byte(`<!doctype html><html><head><link rel="stylesheet" href="theme.css"><script type="module" src="main.js"></script></head><body><h1>Sandbox fixture</h1><img id="preview" src="../preview.png"><button id="navigate">Open node</button><button id="fail">Simulate error</button><pre id="snapshot"></pre></body></html>`),
		"dist/theme.css":  []byte(`@font-face{font-family:Fixture;src:url('./font.woff2')}body{font-family:Fixture,sans-serif;padding:24px;background:#f5f7fb;color:#172033}pre{white-space:pre-wrap}button{padding:8px}`),
		"dist/helper.js":  []byte(`export const version = 2;`), "dist/main.js": []byte(themeBrowserScript),
	}
	manifest := model.ThemeManifest{SchemaVersion: 1, ID: themeID, Name: themeID, Description: "Browser bridge test", Version: "1.0.0", Author: "Beat", License: "MIT", Runtime: model.ThemeRuntimeSandboxed, MinimumBeatVersion: "0.1.0", Preview: "preview.png"}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		digest := sha256.Sum256(files[path])
		manifest.Files = append(manifest.Files, model.ThemeManifestFile{Path: path, Size: int64(len(files[path])), SHA256: fmt.Sprintf("%x", digest)})
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["beat-theme.json"] = raw
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

const themeBrowserScript = `import {version} from './helper.js';
import {connectBeatTheme} from './beat-sdk.js';
let snapshots=0;
const client=connectBeatTheme(snapshot=>{
  document.querySelector('#snapshot').textContent=JSON.stringify(snapshot);
  document.body.dataset.snapshots=String(++snapshots);
  document.querySelector('#navigate').onclick=()=>client.navigate('/node/public-node');
  document.querySelector('#fail').onclick=()=>client.reportError('render_failed');
});
try{document.cookie;document.body.dataset.cookies='exposed'}catch{document.body.dataset.cookies='blocked'}
try{localStorage.getItem('secret');document.body.dataset.storage='exposed'}catch{document.body.dataset.storage='blocked'}
fetch('/api/v1/admin/private').then(()=>document.body.dataset.network='exposed').catch(()=>document.body.dataset.network='blocked');
document.body.dataset.module='loaded';
`
