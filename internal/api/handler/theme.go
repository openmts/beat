package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/beat/backend/internal/store"
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
	ctx, actorID, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	pkg, err := handler.service.Install(ctx, raw, "upload", "", actorID)
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
	ctx, actorID, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	pkg, err := handler.service.Activate(ctx, packageID, actorID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, pkg)
}

func (handler *ThemeHandler) HandleRollback(w http.ResponseWriter, request *http.Request) {
	ctx, actorID, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	target, err := handler.service.Rollback(ctx, actorID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, map[string]string{"rollback_target": target})
}

func (handler *ThemeHandler) HandleDelete(w http.ResponseWriter, request *http.Request) {
	packageID := request.PathValue("id")
	ctx, actorID, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	result, err := handler.service.DeletePackage(ctx, packageID, actorID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	if result.CleanupPending {
		JSONResponse(w, http.StatusAccepted, result)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeThemeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrThemeForbidden):
		JSONError(w, http.StatusForbidden, "theme owner session is required")
	case errors.Is(err, store.ErrThemeRecentAuth):
		JSONError(w, http.StatusPreconditionRequired, "recent identity confirmation is required")
	case errors.Is(err, store.ErrThemeMarketNotFound):
		JSONError(w, http.StatusNotFound, "theme market source is unavailable")
	case errors.Is(err, store.ErrThemeMarketSourceID):
		JSONError(w, http.StatusConflict, "theme market source changed; refresh the catalog and retry")
	case errors.Is(err, store.ErrThemePackageConflict):
		JSONError(w, http.StatusConflict, "theme package changed; reload and retry")
	case errors.Is(err, theme.ErrMarketChanged):
		JSONError(w, http.StatusConflict, "the catalog entry changed; refresh the catalog and select the package again")
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
