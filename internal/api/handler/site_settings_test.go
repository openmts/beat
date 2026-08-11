package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/store"
)

func TestSiteSettingsHandler(t *testing.T) {
	sqliteStore, _ := setupNodeTestDB(t)
	handler := NewSiteSettingsHandler(store.NewSiteSettingsStore(sqliteStore.DB))

	get := httptest.NewRecorder()
	handler.HandleGet(get, httptest.NewRequest(http.MethodGet, "/api/v1/settings/site", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"site_title":"Beat Monitor"`) {
		t.Fatalf("get status = %d, body = %s", get.Code, get.Body.String())
	}

	update := httptest.NewRecorder()
	handler.HandleUpdate(update, httptest.NewRequest(http.MethodPut, "/api/v1/settings/site", strings.NewReader(
		`{"site_title":"Status","site_description":"Public status","logo_url":"/logo.svg",`+
			`"favicon_url":"/icon.svg","default_theme":"dark","show_ip_addresses":false,`+
			`"show_network_quality":false}`)))
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"site_title":"Status"`) {
		t.Fatalf("update status = %d, body = %s", update.Code, update.Body.String())
	}

	for _, body := range []string{`{`, `{"site_title":"","default_theme":"system"}`} {
		response := httptest.NewRecorder()
		handler.HandleUpdate(response, httptest.NewRequest(http.MethodPut, "/api/v1/settings/site", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid body %q status = %d", body, response.Code)
		}
	}
}

func TestSiteSettingsHandlerErrorsAndDefaults(t *testing.T) {
	defaultResponse := httptest.NewRecorder()
	NewSiteSettingsHandler(nil).HandleGet(defaultResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/settings/site", nil))
	if defaultResponse.Code != http.StatusOK {
		t.Fatalf("default status = %d", defaultResponse.Code)
	}

	sqliteStore, _ := setupNodeTestDB(t)
	handler := NewSiteSettingsHandler(store.NewSiteSettingsStore(sqliteStore.DB))
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	get := httptest.NewRecorder()
	handler.HandleGet(get, httptest.NewRequest(http.MethodGet, "/api/v1/settings/site", nil))
	if get.Code != http.StatusInternalServerError {
		t.Fatalf("closed get status = %d", get.Code)
	}
	update := httptest.NewRecorder()
	handler.HandleUpdate(update, httptest.NewRequest(http.MethodPut, "/api/v1/settings/site", strings.NewReader(
		`{"site_title":"Status","site_description":"","logo_url":"","favicon_url":"",`+
			`"default_theme":"system","show_ip_addresses":true,"show_network_quality":true}`)))
	if update.Code != http.StatusInternalServerError {
		t.Fatalf("closed update status = %d", update.Code)
	}
}
