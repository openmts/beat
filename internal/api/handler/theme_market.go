package handler

import (
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/theme"
)

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
	ctx, _, ok := themeMutationContext(w, request)
	if !ok {
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
		WriteBodyError(w, err)
		return
	}
	source, err := handler.markets.CreateSource(ctx, &model.ThemeMarketSource{
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
	ctx, _, ok := themeMutationContext(w, request)
	if !ok {
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
		WriteBodyError(w, err)
		return
	}
	source, err := handler.markets.UpdateSource(ctx, id, model.ThemeMarketSource{
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
	ctx, _, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	id := request.PathValue("id")
	if err := handler.markets.DeleteSource(ctx, id); err != nil {
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
	ctx, _, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	sourceID := request.PathValue("id")
	catalog, err := handler.markets.RefreshCatalog(ctx, sourceID)
	if err != nil {
		writeThemeError(w, err)
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
	ctx, actorID, ok := themeMutationContext(w, request)
	if !ok {
		return
	}
	var body struct {
		Version       string `json:"version"`
		PackageSHA256 string `json:"package_sha256"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	pkg, err := handler.markets.InstallFromMarket(ctx, theme.MarketSelection{SourceID: sourceID,
		ThemeID: themeID, Version: body.Version, PackageSHA256: body.PackageSHA256}, actorID)
	if err != nil {
		writeThemeError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, pkg)
}
