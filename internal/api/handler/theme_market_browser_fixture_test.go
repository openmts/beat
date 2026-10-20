package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestThemeMarketBrowserFixture(t *testing.T) {
	if os.Getenv("BEAT_MARKET_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture not requested")
	}
	_, service, database := setupThemeHandler(t)
	market := newBrowserMarket(t, service, database)
	mux := http.NewServeMux()
	registerThemeFixtureRoutes(mux, market.handler)
	registerBrowserMarketRoutes(mux, market.handler)
	mux.HandleFunc("GET /api/v1/auth/session", fixtureThemeOwner(func(w http.ResponseWriter, r *http.Request) { browserMarketSession(w, r, database) }))
	mux.HandleFunc("GET /fixture/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"fixture": "beat-market-browser-fixture", "catalog_url": market.server.URL + "/catalog",
			"public_key": market.publicKey, "source_id": market.identity, "not_modified": market.notModified.Load()})
	})
	mux.HandleFunc("POST /fixture/catalog/{mode}", fixtureThemeOwner(market.setMode))
	mux.HandleFunc("POST /fixture/require-proof", fixtureThemeOwner(func(w http.ResponseWriter, r *http.Request) {
		if _, err := database.DB.ExecContext(r.Context(), `UPDATE admin_sessions SET reauthenticated_until=NULL WHERE id='s-1'`); err != nil {
			JSONError(w, 500, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/v1/auth/reauthenticate", fixtureThemeOwner(func(w http.ResponseWriter, r *http.Request) { browserMarketProof(w, r, database) }))
	mux.HandleFunc("POST /fixture/cleanup/{action}/{id}", fixtureThemeOwner(market.cleanupControl))
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
	_, _ = fmt.Fprintf(os.Stdout, "MARKET_FIXTURE=%s\n", server.URL)
	select {
	case <-stop:
	case <-time.After(10 * time.Minute):
		t.Fatal("market fixture timed out")
	}
}

func registerBrowserMarketRoutes(mux *http.ServeMux, handler *ThemeHandler) {
	for route, action := range map[string]http.HandlerFunc{
		"GET /api/v1/admin/theme-markets":                                 handler.HandleListMarkets,
		"POST /api/v1/admin/theme-markets":                                handler.HandleCreateMarket,
		"PUT /api/v1/admin/theme-markets/{id}":                            handler.HandleUpdateMarket,
		"DELETE /api/v1/admin/theme-markets/{id}":                         handler.HandleDeleteMarket,
		"GET /api/v1/admin/theme-markets/{id}/catalog":                    handler.HandleRefreshCatalog,
		"POST /api/v1/admin/theme-markets/{id}/themes/{theme_id}/install": handler.HandleInstallFromMarket,
	} {
		mux.HandleFunc(route, fixtureThemeOwner(action))
	}
}

func browserMarketProof(w http.ResponseWriter, request *http.Request, database *store.SQLiteStore) {
	var body struct {
		Password string `json:"password"`
		Code     string `json:"totp_code"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Password != "fixture-owner-proof" {
		JSONError(w, http.StatusBadRequest, "fixture proof is invalid")
		return
	}
	if _, err := database.DB.ExecContext(request.Context(), `UPDATE admin_sessions SET reauthenticated_until=? WHERE id='s-1'`, time.Now().UTC().Add(time.Hour)); err != nil {
		JSONError(w, 500, err.Error())
		return
	}
	session, err := store.NewAdminStore(database.DB).GetSessionByID(request.Context(), "s-1")
	if err != nil {
		JSONError(w, 500, err.Error())
		return
	}
	JSONResponse(w, http.StatusOK, session)
}

func browserMarketSession(w http.ResponseWriter, request *http.Request, database *store.SQLiteStore) {
	users := store.NewAdminStore(database.DB)
	user, err := users.GetUserByID(request.Context(), "owner-1")
	if err != nil {
		JSONError(w, 500, err.Error())
		return
	}
	session, err := users.GetSessionByID(request.Context(), "s-1")
	if err != nil {
		JSONError(w, 500, err.Error())
		return
	}
	JSONResponse(w, http.StatusOK, model.AdminPrincipal{User: *user, Session: *session})
}

func (market *browserMarket) cleanupControl(w http.ResponseWriter, request *http.Request) {
	pkg, err := store.NewThemeStore(market.database.DB).GetPackageByID(request.Context(), request.PathValue("id"))
	if err != nil || pkg == nil {
		JSONError(w, 404, "fixture package missing")
		return
	}
	name := filepath.Join(filepath.Dir(market.service.PackageDir("")), "quarantine", pkg.ArchiveDigest)
	if request.PathValue("action") == "block" {
		// 路径来自本 fixture 临时数据库，只移除本测试创建的隔离副本。
		err = os.RemoveAll(name)
		if err == nil {
			err = os.WriteFile(name, []byte("fixture obstruction"), 0o600)
		}
	} else if request.PathValue("action") == "unblock" {
		err = os.Remove(name)
	} else {
		JSONError(w, 400, "unknown fixture action")
		return
	}
	if err != nil {
		JSONError(w, 500, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
