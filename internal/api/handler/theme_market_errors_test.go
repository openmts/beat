package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestThemeMarketHandlersReturnUnavailableWhenFeatureIsNotConfigured(t *testing.T) {
	_, service, _ := setupThemeHandler(t)
	handler := NewThemeHandler(service, nil)
	for name, method := range map[string]http.HandlerFunc{
		"list": handler.HandleListMarkets, "create": handler.HandleCreateMarket, "update": handler.HandleUpdateMarket,
		"delete": handler.HandleDeleteMarket, "refresh": handler.HandleRefreshCatalog, "install": handler.HandleInstallFromMarket,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			method(response, withJSONBody(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))).WithContext(themeOwnerContext().Context()))
			if response.Code != http.StatusNotFound {
				t.Fatalf("unconfigured market: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestThemeMarketRejectsInvalidBodiesAndUnavailableSourceMutations(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	for _, method := range []http.HandlerFunc{handler.HandleCreateMarket, handler.HandleUpdateMarket} {
		response := httptest.NewRecorder()
		method(response, withJSONBody(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"unknown":true}`))).WithContext(themeOwnerContext().Context()))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid source body: %d %s", response.Code, response.Body.String())
		}
	}
	for _, method := range []http.HandlerFunc{handler.HandleUpdateMarket, handler.HandleDeleteMarket, handler.HandleRefreshCatalog} {
		request := withJSONBody(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"missing"}`))).WithContext(themeOwnerContext().Context())
		request.SetPathValue("id", "absent")
		response := httptest.NewRecorder()
		method(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("missing source: %d %s", response.Code, response.Body.String())
		}
	}
	request := withJSONBody(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"untrusted","catalog_url":"http://example.com/catalog","public_key":"`+base64.StdEncoding.EncodeToString(make([]byte, 32))+`"}`))).WithContext(themeOwnerContext().Context())
	response := httptest.NewRecorder()
	handler.HandleCreateMarket(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("non-HTTPS source accepted: %d", response.Code)
	}
}

func TestThemeMarketInstallHandlesDisabledSourceWithExplicitSelection(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	source, err := handler.markets.CreateSource(t.Context(), &model.ThemeMarketSource{Name: "Disabled", CatalogURL: "https://example.com/catalog", PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	request := withJSONBody(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"version":"1.0.0","package_sha256":"`+strings.Repeat("a", 64)+`"}`))).WithContext(themeOwnerContext().Context())
	request.SetPathValue("id", source.ID)
	request.SetPathValue("theme_id", "selected-theme")
	response := httptest.NewRecorder()
	handler.HandleInstallFromMarket(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled source install: %d %s", response.Code, response.Body.String())
	}
}
