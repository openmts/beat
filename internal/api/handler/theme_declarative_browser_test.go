package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

func TestDeclarativeThemeBrowserFixture(t *testing.T) {
	if os.Getenv("BEAT_DECLARATIVE_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture not requested")
	}
	handler, service, _ := setupThemeHandler(t)
	packages := installBrowserExamples(t, service)
	mux := http.NewServeMux()
	registerThemeFixtureRoutes(mux, handler)
	mux.HandleFunc("GET /fixture/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"fixture": "beat-declarative-browser-fixture", "packages": packages})
	})
	mux.HandleFunc("POST /fixture/activate/{name}", func(w http.ResponseWriter, r *http.Request) {
		pkg, exists := packages[r.PathValue("name")]
		if !exists {
			http.NotFound(w, r)
			return
		}
		if _, err := service.Activate(r.Context(), pkg.ID, "fixture-owner"); err != nil {
			JSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/v1/", declarativeFixtureAPI)
	stop := make(chan struct{})
	var once sync.Once
	mux.HandleFunc("POST /fixture/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		once.Do(func() { close(stop) })
	})
	mux.HandleFunc("/", themeFixtureStatic(t, ""))
	server := httptest.NewServer(middleware.SecurityHeaders(mux))
	t.Cleanup(server.Close)
	_, _ = fmt.Fprintf(os.Stdout, "DECLARATIVE_FIXTURE=%s\n", server.URL)
	select {
	case <-stop:
	case <-time.After(10 * time.Minute):
		t.Fatal("fixture timed out")
	}
}

func themeFixtureStatic(t *testing.T, fallback string) http.HandlerFunc {
	t.Helper()
	static := os.Getenv("BEAT_THEME_BROWSER_STATIC")
	if static == "" {
		static = fallback
	}
	if static == "" {
		t.Fatal("BEAT_THEME_BROWSER_STATIC must point to an isolated frontend build")
	}
	if _, err := os.Stat(filepath.Join(static, "index.html")); err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/themes/") {
			http.FileServer(http.Dir(static)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(static, "index.html"))
	}
}

func installBrowserExamples(t *testing.T, service *theme.Service) map[string]*model.ThemePackage {
	t.Helper()
	packages := make(map[string]*model.ThemePackage)
	for _, name := range []string{"slate", "operations", "canvas"} {
		raw, err := theme.BuildArchive(theme.PackOptions{Source: filepath.Join("../../../themes", name)})
		if err != nil {
			t.Fatal(err)
		}
		pkg, err := service.Install(t.Context(), raw, model.ThemeSourceUpload, "", "fixture-owner")
		if err != nil {
			t.Fatal(err)
		}
		packages[name] = pkg
	}
	return packages
}

func registerThemeFixtureRoutes(mux *http.ServeMux, handler *ThemeHandler) {
	mux.HandleFunc("GET /themes/assets/{digest}/{asset...}", handler.HandleAsset)
	mux.HandleFunc("GET /api/v1/themes/active", handler.HandleActive)
	for route, action := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/themes":                    handler.HandleList,
		"GET /api/v1/admin/themes/{id}/preview":       handler.HandlePreview,
		"PUT /api/v1/admin/themes/{id}/configuration": handler.HandleConfiguration,
		"POST /api/v1/admin/themes/{id}/activate":     handler.HandleActivate,
		"POST /api/v1/admin/themes/built-in":          handler.HandleBuiltIn,
		"POST /api/v1/admin/themes/rollback":          handler.HandleRollback,
		"POST /api/v1/admin/themes/upload":            handler.HandleUpload,
		"GET /api/v1/admin/themes/{id}/export":        handler.HandleExport,
		"DELETE /api/v1/admin/themes/{id}":            handler.HandleDelete,
	} {
		mux.HandleFunc(route, fixtureThemeOwner(action))
	}
}

func fixtureThemeOwner(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Beat-Fixture-Owner") != "theme-preview-fixture" {
			JSONError(w, http.StatusUnauthorized, "fixture owner required")
			return
		}
		next(w, r.WithContext(themeOwnerContext().Context()))
	}
}
