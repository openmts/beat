package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

func TestThemeDeleteReportsCommittedDeletionWhenCleanupIsPending(t *testing.T) {
	handler, service, _ := setupThemeHandler(t)
	pkg, err := service.Install(t.Context(), themeUploadZip(t), model.ThemeSourceUpload, "", "owner")
	if err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(filepath.Dir(service.PackageDir("")), "quarantine", pkg.ArchiveDigest)
	if err := os.WriteFile(blocked, []byte("owned test obstruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/themes/"+pkg.ID, nil).WithContext(themeOwnerContext().Context())
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", pkg.ID)
	response := httptest.NewRecorder()
	handler.HandleDelete(response, request)
	var body struct {
		Data theme.PackageDeletion `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusAccepted || body.Data.State != "deleted" || !body.Data.CleanupPending || body.Data.PackageID != pkg.ID {
		t.Fatalf("ambiguous deletion result: %d %s", response.Code, response.Body.String())
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.HandleDelete(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("cleanup retry: %d %s", response.Code, response.Body.String())
	}
}

func TestThemeMarketInstallRequiresAnExplicitSelectionBeforeFetching(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	for _, input := range []string{"", "{}", `{"version":"1.0.0"}`, `{"version":"1.0.0","package_sha256":"invalid"}`, `{"version":"1.0.0","package_sha256":"` + strings.Repeat("a", 64) + `","unknown":true}`} {
		request := httptest.NewRequest(http.MethodPost, "/install", strings.NewReader(input)).WithContext(themeOwnerContext().Context())
		request.Header.Set("Content-Type", "application/json")
		request.SetPathValue("id", "source-does-not-exist")
		request.SetPathValue("theme_id", "selected-theme")
		response := httptest.NewRecorder()
		handler.HandleInstallFromMarket(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid selection reached source lookup: %d %s", response.Code, response.Body.String())
		}
	}
}
