package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

const maxThemeUploadBytes = 25 << 20

// ThemeHandler 暴露主题公开与管理 API。
type ThemeHandler struct {
	service *theme.Service
	markets *theme.MarketService
}

// NewThemeHandler 构造主题 handler；markets 可为 nil。
func NewThemeHandler(service *theme.Service, markets *theme.MarketService) *ThemeHandler {
	return &ThemeHandler{service: service, markets: markets}
}

func (handler *ThemeHandler) HandleActive(w http.ResponseWriter, request *http.Request) {
	descriptor, err := handler.service.ActiveDescriptor(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "active theme is unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, descriptor)
}

// HandleAsset 从内容寻址目录安全地提供主题资产。
func (handler *ThemeHandler) HandleAsset(w http.ResponseWriter, request *http.Request) {
	digest := request.PathValue("digest")
	assetPath := request.PathValue("asset")
	if !isHexDigest(digest) || assetPath == "" || strings.ContainsRune(assetPath, '\\') {
		http.NotFound(w, request)
		return
	}
	clean := path.Clean("/" + assetPath)
	if strings.HasPrefix(clean, "/..") || strings.HasPrefix(assetPath, "/") {
		http.NotFound(w, request)
		return
	}
	contentRoot := filepath.Join(handler.service.PackageDir(digest), "content")
	full := filepath.Join(contentRoot, filepath.FromSlash(assetPath))
	if filepath.Dir(full) != contentRoot && !strings.HasPrefix(filepath.Dir(full), contentRoot+string(os.PathSeparator)) {
		http.NotFound(w, request)
		return
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, request)
		return
	}
	content, err := os.ReadFile(full)
	if err != nil {
		http.NotFound(w, request)
		return
	}
	mimeType := mimeForPath(assetPath)
	if mimeType == "" {
		http.NotFound(w, request)
		return
	}
	if mimeType == "text/html" {
		w.Header().Set("Content-Security-Policy", sandboxCSP)
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+digest+`"`)
	_, _ = w.Write(content)
}

const sandboxCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'none'; frame-src 'none'; " +
	"worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !isHexCharacter(byte(character)) {
			return false
		}
	}
	return true
}

func isHexCharacter(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'f'
}

func mimeForPath(assetPath string) string {
	switch strings.ToLower(filepath.Ext(assetPath)) {
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".html":
		return "text/html"
	default:
		return ""
	}
}

// --- 管理路由（owner + 最近重认证由路由层保障） ---

func (handler *ThemeHandler) HandleList(w http.ResponseWriter, request *http.Request) {
	packages, err := handler.service.ListPackages(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "themes are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, packages)
}

func (handler *ThemeHandler) HandleUpload(w http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(w, request.Body, maxThemeUploadBytes+1)
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "theme archive is too large or unreadable")
		return
	}
	if len(raw) > maxThemeUploadBytes {
		JSONError(w, http.StatusRequestEntityTooLarge, "theme archive is too large")
		return
	}
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	pkg, err := handler.service.Install(request.Context(), raw, "upload", "", principal.User.ID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, pkg)
}

func (handler *ThemeHandler) HandleExport(w http.ResponseWriter, request *http.Request) {
	packageID := request.PathValue("id")
	raw, filename, err := handler.service.Export(request.Context(), packageID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+url.PathEscape(filename)+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(raw)
}

func (handler *ThemeHandler) HandleActivate(w http.ResponseWriter, request *http.Request) {
	packageID := request.PathValue("id")
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	pkg, err := handler.service.Activate(request.Context(), packageID, principal.User.ID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, pkg)
}

func (handler *ThemeHandler) HandleRollback(w http.ResponseWriter, request *http.Request) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	target, err := handler.service.Rollback(request.Context(), principal.User.ID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, map[string]string{"rollback_target": target})
}

func (handler *ThemeHandler) HandleDelete(w http.ResponseWriter, request *http.Request) {
	packageID := request.PathValue("id")
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := handler.service.Delete(request.Context(), packageID, principal.User.ID); err != nil {
		writeThemeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeThemeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, theme.ErrArchiveInvalid), errors.Is(err, theme.ErrManifestInvalid):
		JSONError(w, http.StatusBadRequest, "theme archive is invalid")
	case errors.Is(err, theme.ErrPackageConflict):
		JSONError(w, http.StatusConflict, "theme package already exists")
	case errors.Is(err, theme.ErrRuntimeIncompatible):
		JSONError(w, http.StatusBadRequest, "theme is not compatible with this server version")
	case errors.Is(err, theme.ErrContentUnavailable):
		JSONError(w, http.StatusNotFound, "theme package is unavailable")
	case errors.Is(err, theme.ErrActivePackage):
		JSONError(w, http.StatusConflict, "the active theme cannot be deleted")
	case errors.Is(err, theme.ErrNoHealthyRollback):
		JSONError(w, http.StatusConflict, "no healthy rollback target exists")
	case errors.Is(err, theme.ErrMarketInvalid):
		JSONError(w, http.StatusBadRequest, "theme market catalog is invalid")
	case errors.Is(err, theme.ErrMarketUnavailable):
		JSONError(w, http.StatusServiceUnavailable, "theme market is unavailable")
	default:
		slog.Error("theme operation failed", "error", err)
		JSONError(w, http.StatusBadRequest, "theme request failed")
	}
}

// --- 市场源管理（owner + 最近重认证） ---

func (handler *ThemeHandler) HandleListMarkets(w http.ResponseWriter, request *http.Request) {
	if handler.markets == nil {
		JSONError(w, http.StatusNotFound, "theme market is unavailable")
		return
	}
	sources, err := handler.markets.ListSources(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "theme markets are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, sources)
}

func (handler *ThemeHandler) HandleCreateMarket(w http.ResponseWriter, request *http.Request) {
	if handler.markets == nil {
		JSONError(w, http.StatusNotFound, "theme market is unavailable")
		return
	}
	var body struct {
		Name           string `json:"name"`
		CatalogURL     string `json:"catalog_url"`
		PublicKey      string `json:"public_key"`
		KeyFingerprint string `json:"key_fingerprint"`
		Enabled        bool   `json:"enabled"`
		SortOrder      int    `json:"sort_order"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid theme market source")
		return
	}
	source, err := handler.markets.CreateSource(request.Context(), &model.ThemeMarketSource{
		Name: body.Name, CatalogURL: body.CatalogURL, PublicKey: body.PublicKey,
		KeyFingerprint: body.KeyFingerprint, Enabled: body.Enabled, SortOrder: body.SortOrder,
	})
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, source)
}

func (handler *ThemeHandler) HandleUpdateMarket(w http.ResponseWriter, request *http.Request) {
	if handler.markets == nil {
		JSONError(w, http.StatusNotFound, "theme market is unavailable")
		return
	}
	id := request.PathValue("id")
	var body struct {
		Name           string `json:"name"`
		CatalogURL     string `json:"catalog_url"`
		PublicKey      string `json:"public_key"`
		KeyFingerprint string `json:"key_fingerprint"`
		Enabled        bool   `json:"enabled"`
		SortOrder      int    `json:"sort_order"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid theme market source")
		return
	}
	source, err := handler.markets.UpdateSource(request.Context(), id, model.ThemeMarketSource{
		Name: body.Name, CatalogURL: body.CatalogURL, PublicKey: body.PublicKey,
		KeyFingerprint: body.KeyFingerprint, Enabled: body.Enabled, SortOrder: body.SortOrder,
	})
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, source)
}

func (handler *ThemeHandler) HandleDeleteMarket(w http.ResponseWriter, request *http.Request) {
	if handler.markets == nil {
		JSONError(w, http.StatusNotFound, "theme market is unavailable")
		return
	}
	id := request.PathValue("id")
	if err := handler.markets.DeleteSource(request.Context(), id); err != nil {
		writeThemeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *ThemeHandler) HandleRefreshCatalog(w http.ResponseWriter, request *http.Request) {
	if handler.markets == nil {
		JSONError(w, http.StatusNotFound, "theme market is unavailable")
		return
	}
	sourceID := request.PathValue("id")
	catalog, err := handler.markets.RefreshCatalog(request.Context(), sourceID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	if catalog == nil {
		JSONResponse(w, http.StatusOK, map[string]any{"cached": true})
		return
	}
	JSONResponse(w, http.StatusOK, catalog)
}

func (handler *ThemeHandler) HandleInstallFromMarket(w http.ResponseWriter, request *http.Request) {
	if handler.markets == nil {
		JSONError(w, http.StatusNotFound, "theme market is unavailable")
		return
	}
	sourceID := request.PathValue("id")
	themeID := request.PathValue("theme_id")
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	pkg, err := handler.markets.InstallFromMarket(request.Context(), sourceID, themeID, principal.User.ID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, pkg)
}
