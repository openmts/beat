package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestThemeConfigurationHandlerErrorsAndNoStore(t *testing.T) {
	handler, service, database := setupThemeHandler(t)
	pkg, err := service.Install(t.Context(), themeUploadZip(t), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	preview := httptest.NewRequest(http.MethodGet, "/preview", nil)
	preview.SetPathValue("id", pkg.ID)
	response := httptest.NewRecorder()
	handler.HandlePreview(response, preview)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("preview: %d %s", response.Code, response.Body.String())
	}
	for _, body := range []string{`{"radius":9}`, strings.Repeat(" ", 17<<10)} {
		request := httptest.NewRequest(http.MethodPut, "/config", strings.NewReader(body)).WithContext(themeOwnerContext().Context())
		request.SetPathValue("id", pkg.ID)
		response := httptest.NewRecorder()
		handler.HandleConfiguration(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("bad config: %d", response.Code)
		}
	}
	unauth := httptest.NewRecorder()
	handler.HandleBuiltIn(unauth, httptest.NewRequest(http.MethodPost, "/built-in", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth recovery: %d", unauth.Code)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.HandlePreview(response, preview)
	if response.Code == http.StatusOK {
		t.Fatal("closed store preview succeeded")
	}
	recovery := httptest.NewRequest(http.MethodPost, "/built-in", bytes.NewReader(nil)).WithContext(themeOwnerContext().Context())
	response = httptest.NewRecorder()
	handler.HandleBuiltIn(response, recovery)
	if response.Code == http.StatusOK {
		t.Fatal("closed store recovery succeeded")
	}
}
