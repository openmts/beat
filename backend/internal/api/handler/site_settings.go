package handler

import (
	"context"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type SiteSettingsHandler struct {
	store *store.SiteSettingsStore
}

func NewSiteSettingsHandler(settingsStore *store.SiteSettingsStore) *SiteSettingsHandler {
	return &SiteSettingsHandler{store: settingsStore}
}

func (handler *SiteSettingsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	settings, err := loadSiteSettings(r.Context(), handler.store)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load site settings")
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

func (handler *SiteSettingsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings model.SiteSettings
	if err := ParseJSON(r, &settings); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := handler.store.Update(r.Context(), settings)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update site settings")
		return
	}
	JSONResponse(w, http.StatusOK, updated)
}

func loadSiteSettings(
	ctx context.Context,
	settingsStore *store.SiteSettingsStore,
) (model.SiteSettings, error) {
	if settingsStore == nil {
		return model.DefaultSiteSettings(), nil
	}
	return settingsStore.Get(ctx)
}

func publicNode(node model.Node, settings model.SiteSettings) model.Node {
	if !settings.ShowIPAddresses {
		node.Host = ""
	}
	return node
}
