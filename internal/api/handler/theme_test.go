package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/theme"
)

func setupThemeHandler(t *testing.T) (*ThemeHandler, *theme.Service, *store.SQLiteStore) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	service, err := theme.NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	markets := theme.NewMarketService(store.NewThemeStore(sqlite.DB),
		theme.NewMarketClient(true, time.Now), service, time.Now)
	return NewThemeHandler(service, markets), service, sqlite
}

func themeUploadManifest() string {
	files := []model.ThemeManifestFile{
		{Path: "preview.png", Size: 1, SHA256: sha256HexBytes([]byte("p"))},
		{Path: "assets/theme.css", Size: 1, SHA256: sha256HexBytes([]byte("c"))},
	}
	raw, _ := json.Marshal(map[string]any{
		"schema_version": 1, "id": "test-theme", "name": "Test Theme",
		"description": "desc", "version": "1.0.0", "author": "a", "license": "MIT",
		"runtime": "declarative", "minimum_beat_version": "0.1.0",
		"preview": "preview.png", "files": files,
	})
	return string(raw)
}

func themeUploadZip(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	manifest := themeUploadManifest()
	entries := map[string]string{
		"beat-theme.json":  manifest,
		"preview.png":      "p",
		"assets/theme.css": "c",
	}
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func sha256HexBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return fmt.Sprintf("%x", digest)
}

func themeOwnerContext() *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	return request.WithContext(middleware.WithAdminPrincipal(request.Context(), model.AdminPrincipal{
		User: model.AdminUser{ID: "owner-1"}, Session: model.AdminSession{ID: "s-1"},
	}))
}

func TestThemeHandlerActiveAndAssetServing(t *testing.T) {
	handler, service, _ := setupThemeHandler(t)
	ctx := context.Background()

	// 未激活时返回内置主题
	request := httptest.NewRequest(http.MethodGet, "/api/v1/themes/active", nil)
	response := httptest.NewRecorder()
	handler.HandleActive(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"built_in":true`) {
		t.Fatalf("active = %d %s", response.Code, response.Body.String())
	}

	raw := themeUploadZip(t)
	pkg, err := service.Install(ctx, raw, model.ThemeSourceUpload, "", "owner-1")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// 资产服务
	assetRequest := httptest.NewRequest(http.MethodGet,
		"/themes/assets/"+pkg.ArchiveDigest+"/assets/theme.css", nil)
	assetRequest.SetPathValue("digest", pkg.ArchiveDigest)
	assetRequest.SetPathValue("asset", "assets/theme.css")
	assetResponse := httptest.NewRecorder()
	handler.HandleAsset(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusOK || assetResponse.Body.String() != "c" {
		t.Fatalf("asset = %d %q", assetResponse.Code, assetResponse.Body.String())
	}
	if assetResponse.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff header")
	}
	// 非法路径拒绝
	for _, bad := range []struct{ digest, asset string }{
		{"not-a-digest", "x.css"},
		{pkg.ArchiveDigest, "../archive.zip"},
		{pkg.ArchiveDigest, "missing.css"},
	} {
		badRequest := httptest.NewRequest(http.MethodGet, "/themes/assets/x", nil)
		badRequest.SetPathValue("digest", bad.digest)
		badRequest.SetPathValue("asset", bad.asset)
		badResponse := httptest.NewRecorder()
		handler.HandleAsset(badResponse, badRequest)
		if badResponse.Code != http.StatusNotFound {
			t.Fatalf("bad asset %q status = %d", bad, badResponse.Code)
		}
	}
}

func TestThemeHandlerUploadActivateRollbackDelete(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)

	upload := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/upload",
		bytes.NewReader(themeUploadZip(t)))
	upload = upload.WithContext(themeOwnerContext().Context())
	upload.Header.Set("Content-Type", "application/zip")
	response := httptest.NewRecorder()
	handler.HandleUpload(response, upload)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", response.Code, response.Body.String())
	}
	var created struct {
		Data struct {
			ID      string `json:"id"`
			ThemeID string `json:"theme_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Data.ID == "" {
		t.Fatalf("decode upload = %v", err)
	}

	// 无效上传
	badUpload := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/upload",
		bytes.NewReader([]byte("not-a-zip")))
	badUpload = badUpload.WithContext(themeOwnerContext().Context())
	badResponse := httptest.NewRecorder()
	handler.HandleUpload(badResponse, badUpload)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("bad upload = %d", badResponse.Code)
	}

	// 激活
	activate := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/"+created.Data.ID+"/activate", nil)
	activate.SetPathValue("id", created.Data.ID)
	activate = activate.WithContext(themeOwnerContext().Context())
	activateResponse := httptest.NewRecorder()
	handler.HandleActivate(activateResponse, activate)
	if activateResponse.Code != http.StatusOK {
		t.Fatalf("activate = %d %s", activateResponse.Code, activateResponse.Body.String())
	}

	// 活动包删除冲突
	del := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/themes/"+created.Data.ID, nil)
	del.SetPathValue("id", created.Data.ID)
	del = del.WithContext(themeOwnerContext().Context())
	delResponse := httptest.NewRecorder()
	handler.HandleDelete(delResponse, del)
	if delResponse.Code != http.StatusConflict {
		t.Fatalf("delete active = %d", delResponse.Code)
	}

	// 回滚
	rollback := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/rollback", nil)
	rollback = rollback.WithContext(themeOwnerContext().Context())
	rollbackResponse := httptest.NewRecorder()
	handler.HandleRollback(rollbackResponse, rollback)
	if rollbackResponse.Code != http.StatusOK {
		t.Fatalf("rollback = %d %s", rollbackResponse.Code, rollbackResponse.Body.String())
	}

	// 导出
	export := httptest.NewRequest(http.MethodGet, "/api/v1/admin/themes/"+created.Data.ID+"/export", nil)
	export.SetPathValue("id", created.Data.ID)
	export = export.WithContext(themeOwnerContext().Context())
	exportResponse := httptest.NewRecorder()
	handler.HandleExport(exportResponse, export)
	if exportResponse.Code != http.StatusOK || exportResponse.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("export = %d", exportResponse.Code)
	}

	// 删除
	del = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/themes/"+created.Data.ID, nil)
	del.SetPathValue("id", created.Data.ID)
	del = del.WithContext(themeOwnerContext().Context())
	delResponse = httptest.NewRecorder()
	handler.HandleDelete(delResponse, del)
	if delResponse.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", delResponse.Code)
	}
}

func TestThemeHandlerMarketSources(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	ctx := themeOwnerContext().Context()

	create := httptest.NewRequest(http.MethodPost, "/api/v1/admin/theme-markets",
		strings.NewReader(`{"name":"Market","catalog_url":"https://market.example/c","public_key":"`+strings.Repeat("k", 44)+`"}`))
	create = create.WithContext(ctx)
	createResponse := httptest.NewRecorder()
	handler.HandleCreateMarket(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create market = %d %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(createResponse.Body.Bytes(), &created)

	list := httptest.NewRequest(http.MethodGet, "/api/v1/admin/theme-markets", nil)
	list = list.WithContext(ctx)
	listResponse := httptest.NewRecorder()
	handler.HandleListMarkets(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list markets = %d", listResponse.Code)
	}

	update := httptest.NewRequest(http.MethodPut, "/api/v1/admin/theme-markets/"+created.Data.ID,
		strings.NewReader(`{"name":"Renamed","enabled":false}`))
	update.SetPathValue("id", created.Data.ID)
	update = update.WithContext(ctx)
	updateResponse := httptest.NewRecorder()
	handler.HandleUpdateMarket(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update market = %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/theme-markets/"+created.Data.ID, nil)
	del.SetPathValue("id", created.Data.ID)
	del = del.WithContext(ctx)
	delResponse := httptest.NewRecorder()
	handler.HandleDeleteMarket(delResponse, del)
	if delResponse.Code != http.StatusNoContent {
		t.Fatalf("delete market = %d", delResponse.Code)
	}
}

func TestThemeHandlerErrorPathsWithClosedStore(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	service, err := theme.NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	markets := theme.NewMarketService(store.NewThemeStore(sqlite.DB),
		theme.NewMarketClient(true, time.Now), service, time.Now)
	handler := NewThemeHandler(service, markets)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	ctx := themeOwnerContext().Context()

	list := httptest.NewRequest(http.MethodGet, "/api/v1/admin/themes", nil).WithContext(ctx)
	listResponse := httptest.NewRecorder()
	handler.HandleList(listResponse, list)
	if listResponse.Code != http.StatusInternalServerError {
		t.Fatalf("list = %d", listResponse.Code)
	}
	active := httptest.NewRequest(http.MethodGet, "/api/v1/themes/active", nil)
	activeResponse := httptest.NewRecorder()
	handler.HandleActive(activeResponse, active)
	if activeResponse.Code != http.StatusInternalServerError {
		t.Fatalf("active = %d", activeResponse.Code)
	}
	upload := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/upload",
		bytes.NewReader(themeUploadZip(t))).WithContext(ctx)
	uploadResponse := httptest.NewRecorder()
	handler.HandleUpload(uploadResponse, upload)
	if uploadResponse.Code != http.StatusBadRequest {
		t.Fatalf("upload = %d", uploadResponse.Code)
	}
}

func TestThemeHandlerAllMethodsClosedStore(t *testing.T) {
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	service, err := theme.NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	markets := theme.NewMarketService(store.NewThemeStore(sqlite.DB),
		theme.NewMarketClient(true, time.Now), service, time.Now)
	handler := NewThemeHandler(service, markets)
	if err := sqlite.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}
	ctx := themeOwnerContext().Context()
	for name, request := range map[string]*http.Request{
		"activate": httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/p1/activate", nil),
		"rollback": httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/rollback", nil),
		"delete":   httptest.NewRequest(http.MethodDelete, "/api/v1/admin/themes/p1", nil),
		"export":   httptest.NewRequest(http.MethodGet, "/api/v1/admin/themes/p1/export", nil),
		"markets":  httptest.NewRequest(http.MethodGet, "/api/v1/admin/theme-markets", nil),
		"create_mk": httptest.NewRequest(http.MethodPost, "/api/v1/admin/theme-markets",
			strings.NewReader(`{"name":"M","catalog_url":"https://m.example/c","public_key":"k"}`)),
		"refresh": httptest.NewRequest(http.MethodGet, "/api/v1/admin/theme-markets/m1/catalog", nil),
		"install": httptest.NewRequest(http.MethodPost, "/api/v1/admin/theme-markets/m1/themes/t/install", nil),
	} {
		request = request.WithContext(ctx)
		request.SetPathValue("id", "p1")
		request.SetPathValue("theme_id", "t")
		response := httptest.NewRecorder()
		switch name {
		case "activate":
			handler.HandleActivate(response, request)
		case "rollback":
			handler.HandleRollback(response, request)
		case "delete":
			handler.HandleDelete(response, request)
		case "export":
			handler.HandleExport(response, request)
		case "markets":
			handler.HandleListMarkets(response, request)
		case "create_mk":
			handler.HandleCreateMarket(response, request)
		case "refresh":
			handler.HandleRefreshCatalog(response, request)
		case "install":
			handler.HandleInstallFromMarket(response, request)
		}
		if response.Code == http.StatusOK || response.Code == http.StatusNoContent || response.Code == http.StatusCreated {
			t.Fatalf("%s on closed store returned %d", name, response.Code)
		}
	}
}

func TestWriteThemeErrorStatusMapping(t *testing.T) {
	cases := map[error]int{
		theme.ErrArchiveInvalid:      http.StatusBadRequest,
		theme.ErrManifestInvalid:     http.StatusBadRequest,
		theme.ErrPackageConflict:     http.StatusConflict,
		theme.ErrRuntimeIncompatible: http.StatusBadRequest,
		theme.ErrContentUnavailable:  http.StatusNotFound,
		theme.ErrActivePackage:       http.StatusConflict,
		theme.ErrNoHealthyRollback:   http.StatusConflict,
		theme.ErrMarketInvalid:       http.StatusBadRequest,
		theme.ErrMarketUnavailable:   http.StatusServiceUnavailable,
		errors.New("unexpected"):     http.StatusBadRequest,
	}
	for err, want := range cases {
		response := httptest.NewRecorder()
		writeThemeError(response, err)
		if response.Code != want {
			t.Fatalf("writeThemeError(%v) = %d, want %d", err, response.Code, want)
		}
	}
}

func TestThemeHelperFunctions(t *testing.T) {
	if !isHexDigest(strings.Repeat("a", 64)) || isHexDigest("abc") || isHexDigest(strings.Repeat("g", 64)) {
		t.Fatal("isHexDigest misbehaves")
	}
	for _, valid := range []byte{'0', '9', 'a', 'f', 'z', '-'} {
		_ = valid
	}
}

func TestThemeHandlerAssetInvalidPaths(t *testing.T) {
	handler, service, _ := setupThemeHandler(t)
	raw := themeUploadZip(t)
	pkg, err := service.Install(context.Background(), raw, model.ThemeSourceUpload, "", "owner-1")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// 无效 digest / 缺失路径 / 非法 asset
	for _, tc := range []struct{ digest, asset string }{
		{"not-a-digest", "x.css"},
		{"", "assets/theme.css"},
		{pkg.ArchiveDigest, ""},
		{pkg.ArchiveDigest, "assets/..%2f..%2farchive.zip"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/themes/assets/x", nil)
		request.SetPathValue("digest", tc.digest)
		request.SetPathValue("asset", tc.asset)
		response := httptest.NewRecorder()
		handler.HandleAsset(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("asset %q/%q = %d, want 404", tc.digest, tc.asset, response.Code)
		}
	}
}

func TestThemeHandlerUploadTooLarge(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/themes/upload",
		bytes.NewReader(make([]byte, maxThemeUploadBytes+1)))
	request = request.WithContext(themeOwnerContext().Context())
	response := httptest.NewRecorder()
	handler.HandleUpload(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload too large = %d", response.Code)
	}
}

func TestThemeHandlerMarketSourceSuccess(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	ctx := themeOwnerContext().Context()

	create := httptest.NewRequest(http.MethodPost, "/api/v1/admin/theme-markets",
		strings.NewReader(`{"name":"Market","catalog_url":"https://market.example/c","public_key":"`+
			strings.Repeat("k", 44)+`","enabled":true}`))
	create = create.WithContext(ctx)
	createResponse := httptest.NewRecorder()
	handler.HandleCreateMarket(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(createResponse.Body.Bytes(), &created)

	list := httptest.NewRequest(http.MethodGet, "/api/v1/admin/theme-markets", nil).WithContext(ctx)
	listResponse := httptest.NewRecorder()
	handler.HandleListMarkets(listResponse, list)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "Market") {
		t.Fatalf("list = %d %s", listResponse.Code, listResponse.Body.String())
	}

	update := httptest.NewRequest(http.MethodPut, "/api/v1/admin/theme-markets/"+created.Data.ID,
		strings.NewReader(`{"name":"Renamed","enabled":false}`))
	update.SetPathValue("id", created.Data.ID)
	update = update.WithContext(ctx)
	updateResponse := httptest.NewRecorder()
	handler.HandleUpdateMarket(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update = %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	refresh := httptest.NewRequest(http.MethodGet, "/api/v1/admin/theme-markets/"+created.Data.ID+"/catalog", nil)
	refresh.SetPathValue("id", created.Data.ID)
	refresh = refresh.WithContext(ctx)
	refreshResponse := httptest.NewRecorder()
	handler.HandleRefreshCatalog(refreshResponse, refresh)
	// 无法到达市场 → 503
	if refreshResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("refresh unreachable = %d", refreshResponse.Code)
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/theme-markets/"+created.Data.ID, nil)
	del.SetPathValue("id", created.Data.ID)
	del = del.WithContext(ctx)
	delResponse := httptest.NewRecorder()
	handler.HandleDeleteMarket(delResponse, del)
	if delResponse.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", delResponse.Code)
	}
}

func TestThemeHandlerMarketFullFlow(t *testing.T) {
	// 真实签名目录 + 包服务器，走 handler 完整市场流程
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	packageBytes := themeUploadZip(t)
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
	service, err := theme.NewService(store.NewThemeStore(sqlite.DB), store.NewAdminStore(sqlite.DB),
		filepath.Join(t.TempDir(), "themes"), "0.1.0", time.Now)
	if err != nil {
		t.Fatalf("new theme service: %v", err)
	}
	client := theme.NewMarketClient(true, time.Now)
	markets := theme.NewMarketService(store.NewThemeStore(sqlite.DB), client, service, time.Now)
	handler := NewThemeHandler(service, markets)
	ctx := themeOwnerContext().Context()

	// 先建源
	source, err := markets.CreateSource(ctx, &model.ThemeMarketSource{
		Name: "Market", CatalogURL: "https://placeholder.invalid/c",
		PublicKey: base64.StdEncoding.EncodeToString(publicKey), KeyFingerprint: "fp", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	catalog := model.ThemeCatalog{
		SchemaVersion: 1, SourceID: source.ID,
		Themes: []model.ThemeCatalogEntry{{
			ThemeID: "test-theme", Name: "Test Theme", Version: "1.0.0", Author: "a",
			Description: "d", Runtime: model.ThemeRuntimeDeclarative, PackageURL: packageServer.URL,
			PackageSHA256: packageDigest, MinimumBeat: "0.1.0",
		}},
	}
	body, _ := json.Marshal(catalog)
	signature := ed25519.Sign(privateKey, body)
	catalogServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Beat-Catalog-Signature", base64.StdEncoding.EncodeToString(signature))
		_, _ = w.Write(body)
	}))
	defer catalogServer.Close()
	pool := x509.NewCertPool()
	pool.AddCert(catalogServer.Certificate())
	pool.AddCert(packageServer.Certificate())
	client.SetRootCAs(pool)

	source.CatalogURL = catalogServer.URL
	if _, err := markets.UpdateSource(ctx, source.ID, *source); err != nil {
		t.Fatalf("update source url: %v", err)
	}

	// 刷新目录（成功）
	refresh := httptest.NewRequest(http.MethodGet, "/api/v1/admin/theme-markets/"+source.ID+"/catalog", nil)
	refresh.SetPathValue("id", source.ID)
	refresh = refresh.WithContext(ctx)
	refreshResponse := httptest.NewRecorder()
	handler.HandleRefreshCatalog(refreshResponse, refresh)
	if refreshResponse.Code != http.StatusOK || !strings.Contains(refreshResponse.Body.String(), "Test Theme") {
		t.Fatalf("refresh = %d %s", refreshResponse.Code, refreshResponse.Body.String())
	}

	// 市场安装（成功）
	install := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/theme-markets/"+source.ID+"/themes/test-theme/install", nil)
	install.SetPathValue("id", source.ID)
	install.SetPathValue("theme_id", "test-theme")
	install = install.WithContext(ctx)
	installResponse := httptest.NewRecorder()
	handler.HandleInstallFromMarket(installResponse, install)
	if installResponse.Code != http.StatusCreated {
		t.Fatalf("install = %d %s", installResponse.Code, installResponse.Body.String())
	}
}
