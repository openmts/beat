package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/theme"
)

func setupThemeRouter(t *testing.T) (*Router, *theme.Service) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	adminStore := store.NewAdminStore(sqlite.DB)
	secrets := newTestSecrets(t, sqlite.DB)
	security := newTestSecurity(t, adminStore, secrets)

	themeService, err := theme.NewService(store.NewThemeStore(sqlite.DB), adminStore,
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	markets := theme.NewMarketService(store.NewThemeStore(sqlite.DB),
		theme.NewMarketClient(true, time.Now), themeService, time.Now)

	router := NewRouter(store.NewNodeStore(sqlite.DB), store.NewGroupStore(sqlite.DB),
		store.NewSSHKeyStore(sqlite.DB), store.NewAlertRuleStore(sqlite.DB),
		store.NewAlertChannelStore(sqlite.DB), store.NewAlertEventStore(sqlite.DB), nil,
		WithAdminToken("admin-secret"), WithAdminSecurity(security), WithThemes(themeService, markets))

	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d", bootstrap.Code)
	}
	return router, themeService
}

func routerLoginCookie(router *Router) (*http.Cookie, error) {
	login := requestRouter(router, http.MethodPost, "/api/v1/auth/login",
		`{"username":"owner","password":"correct horse battery staple"}`, "", "http://example.com")
	if login.Code != http.StatusOK {
		return nil, fmt.Errorf("login status %d", login.Code)
	}
	return login.Result().Cookies()[0], nil
}

func themeRouterZip(t *testing.T) []byte {
	t.Helper()
	files := []model.ThemeManifestFile{
		{Path: "preview.png", Size: int64(len(routerThemePNG)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(routerThemePNG)))},
		{Path: "assets/theme.css", Size: 1, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("c")))},
		{Path: "theme.json", Size: int64(len(routerThemeDefinitionJSON)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(routerThemeDefinitionJSON)))},
	}
	manifest, _ := json.Marshal(map[string]any{
		"schema_version": 1, "id": "router-theme", "name": "Router Theme",
		"description": "desc", "version": "1.0.0", "author": "a", "license": "MIT",
		"runtime": "declarative", "minimum_beat_version": "0.1.0",
		"preview": "preview.png", "files": files,
	})
	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	for name, content := range map[string]string{
		"beat-theme.json": string(manifest), "preview.png": routerThemePNG, "assets/theme.css": "c", "theme.json": routerThemeDefinitionJSON,
	} {
		entry, _ := zipWriter.Create(name)
		_, _ = entry.Write([]byte(content))
	}
	_ = zipWriter.Close()
	return buffer.Bytes()
}

func TestRouterThemePublicAndOwnerBoundaries(t *testing.T) {
	router, service := setupThemeRouter(t)

	// 公开活动主题端点无需登录
	public := requestRouter(router, http.MethodGet, "/api/v1/themes/active", "", "", "")
	if public.Code != http.StatusOK || !strings.Contains(public.Body.String(), `"built_in":true`) {
		t.Fatalf("public active = %d %s", public.Code, public.Body.String())
	}

	// 未认证上传拒绝
	unauth := requestRouter(router, http.MethodPost, "/api/v1/admin/themes/upload", "", "", "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized upload = %d", unauth.Code)
	}

	// 引导 owner 但未重认证：上传被拒
	bootstrap := requestRouter(router, http.MethodGet, "/api/v1/auth/state", "", "", "")
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("state = %d", bootstrap.Code)
	}
	cookie, err := routerLoginCookie(router)
	if err != nil {
		t.Fatalf("login cookie: %v", err)
	}
	withoutReauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/themes/upload",
		string(themeRouterZip(t)), cookie, "http://example.com")
	if withoutReauth.Code != http.StatusPreconditionRequired {
		t.Fatalf("upload without reauth = %d", withoutReauth.Code)
	}

	// 重认证后上传成功
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":""}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauthenticate = %d", reauth.Code)
	}
	upload := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/themes/upload",
		string(themeRouterZip(t)), cookie, "http://example.com")
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", upload.Code, upload.Body.String())
	}

	// 资产路由按生产装配挂载：SPA 兜底不得吞掉 /themes/assets 前缀
	pkgs, err := service.ListPackages(t.Context())
	if err != nil || len(pkgs) != 1 {
		t.Fatalf("packages = %v / %v", pkgs, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", router.ServeHandler())
	router.MountThemeAssets(mux)
	fallback := 0
	mux.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) { fallback++; http.NotFound(w, request) })

	asset := httptest.NewRequest(http.MethodGet,
		"http://example.com/themes/assets/"+pkgs[0].ArchiveDigest+"/assets/theme.css", nil)
	assetResponse := httptest.NewRecorder()
	mux.ServeHTTP(assetResponse, asset)
	if assetResponse.Code != http.StatusOK || assetResponse.Body.String() != "c" {
		t.Fatalf("asset = %d %q", assetResponse.Code, assetResponse.Body.String())
	}
	if fallback != 0 {
		t.Fatalf("SPA fallback swallowed the theme asset %d time(s)", fallback)
	}

	// 资产路由不得遮蔽 API
	apiShadow := httptest.NewRequest(http.MethodGet,
		"http://example.com/themes/assets/"+pkgs[0].ArchiveDigest+"/../../api/v1/themes/active", nil)
	shadowResponse := httptest.NewRecorder()
	mux.ServeHTTP(shadowResponse, apiShadow)
	if shadowResponse.Code == http.StatusOK && strings.Contains(shadowResponse.Body.String(), "built_in") {
		t.Fatal("asset route shadowed the API")
	}

	// 预览图与沙箱入口同属该前缀：主题卡片与 iframe 都依赖它可读
	preview := httptest.NewRecorder()
	mux.ServeHTTP(preview, httptest.NewRequest(http.MethodGet,
		"http://example.com/themes/assets/"+pkgs[0].ArchiveDigest+"/preview.png", nil))
	if preview.Code != http.StatusOK || preview.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(preview.Header().Get("Cache-Control"), "public, max-age=31536000") {
		t.Fatalf("preview = %d %q %q", preview.Code, preview.Header().Get("Content-Type"), preview.Header().Get("Cache-Control"))
	}
}

func TestMountThemeAssetsWithoutThemeService(t *testing.T) {
	(&Router{}).MountThemeAssets(nil)
	mux := http.NewServeMux()
	(&Router{}).MountThemeAssets(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.com/themes/assets/x/y.css", nil))
	if response.Code != http.StatusTeapot {
		t.Fatalf("root = %d", response.Code)
	}
}
