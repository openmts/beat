package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestThemePreviewConfigurationAndBuiltInRecovery(t *testing.T) {
	router, service := setupThemeRouter(t)
	pkg, err := service.Install(t.Context(), themeRouterZip(t), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	cookie, err := routerLoginCookie(router)
	if err != nil {
		t.Fatal(err)
	}
	previewPath := "/api/v1/admin/themes/" + pkg.ID + "/preview"
	assertThemeAnonymousBoundary(t, router, previewPath)
	preview := requestRouterWithCookie(router, http.MethodGet, previewPath, "", cookie, "http://example.com")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"declarative"`) {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	active, err := service.ActiveDescriptor(t.Context())
	if err != nil || !active.BuiltIn {
		t.Fatalf("preview changed activation: %+v %v", active, err)
	}
	configPath := "/api/v1/admin/themes/" + pkg.ID + "/configuration"
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatal(reauth.Body.String())
	}
	updated := requestRouterWithCookie(router, http.MethodPut, configPath, `{"overview":"table","detail":"compact","radius":0}`, cookie, "http://example.com")
	var response struct {
		Data model.ActiveThemeDescriptor `json:"data"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if updated.Code != http.StatusOK || response.Data.Declarative == nil || response.Data.Declarative.Overview != "table" || response.Data.Declarative.Radius != 0 {
		t.Fatalf("configuration: %d %s", updated.Code, updated.Body.String())
	}
	if _, err := service.Activate(t.Context(), pkg.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	recovery := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/themes/built-in", "", cookie, "http://example.com")
	if recovery.Code != http.StatusOK {
		t.Fatalf("recovery: %d %s", recovery.Code, recovery.Body.String())
	}
	active, err = service.ActiveDescriptor(t.Context())
	if err != nil || !active.BuiltIn {
		t.Fatalf("recovery: %+v %v", active, err)
	}
}

func assertThemeAnonymousBoundary(t *testing.T, router *Router, previewPath string) {
	t.Helper()
	for _, path := range []string{previewPath, "/api/v1/admin/themes/built-in"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "built-in") {
			method = http.MethodPost
		}
		response := requestRouter(router, method, path, "", "", "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, response.Code)
		}
	}
}

func TestThemeConfigurationRequiresProof(t *testing.T) {
	router, _ := setupThemeRouter(t)
	cookie, err := routerLoginCookie(router)
	if err != nil {
		t.Fatal(err)
	}
	configPath := "/api/v1/admin/themes/example/configuration"
	for _, path := range []string{configPath, "/api/v1/admin/themes/built-in"} {
		method := http.MethodPut
		if strings.HasSuffix(path, "built-in") {
			method = http.MethodPost
		}
		response := requestRouterWithCookie(router, method, path, `{}`, cookie, "http://example.com")
		if response.Code != http.StatusPreconditionRequired {
			t.Fatalf("missing proof %s: %d", path, response.Code)
		}
	}
}
