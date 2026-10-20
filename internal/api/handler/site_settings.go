package handler

import (
	"context"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type SiteSettingsHandler struct {
	store     *store.SiteSettingsStore
	telemetry PublicTelemetryPolicyReader
}

func NewSiteSettingsHandler(settingsStore *store.SiteSettingsStore) *SiteSettingsHandler {
	return &SiteSettingsHandler{store: settingsStore}
}

// WithPublicTelemetry 注入匿名聚合遥测策略；未注入时公共响应报告为关闭。
func (handler *SiteSettingsHandler) WithPublicTelemetry(telemetry PublicTelemetryPolicyReader) *SiteSettingsHandler {
	handler.telemetry = telemetry
	return handler
}

// publicTelemetryView 是公共响应允许出现的展示字段；来源列表、样本与操作者永不下发。
type publicTelemetryView struct {
	Enabled       bool     `json:"enabled"`
	AllowedEvents []string `json:"allowed_events"`
	SampleCeiling int      `json:"sample_ceiling"`
}

type siteSettingsResponse struct {
	model.SiteSettings
	PublicTelemetry publicTelemetryView `json:"public_telemetry"`
}

func (handler *SiteSettingsHandler) publicResponse(ctx context.Context, settings model.SiteSettings) siteSettingsResponse {
	response := siteSettingsResponse{SiteSettings: settings,
		PublicTelemetry: publicTelemetryView{Enabled: false, AllowedEvents: []string{}, SampleCeiling: 0}}
	if handler.telemetry == nil {
		return response
	}
	policy, err := handler.telemetry.Get(ctx)
	if err != nil {
		return response
	}
	response.PublicTelemetry = publicTelemetryView{Enabled: policy.Enabled, AllowedEvents: policy.AllowedEvents,
		SampleCeiling: policy.SampleCeiling}
	if response.PublicTelemetry.AllowedEvents == nil {
		response.PublicTelemetry.AllowedEvents = []string{}
	}
	return response
}

func (handler *SiteSettingsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	settings, err := loadSiteSettings(r.Context(), handler.store)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load site settings")
		return
	}
	JSONResponse(w, http.StatusOK, handler.publicResponse(r.Context(), settings))
}

func (handler *SiteSettingsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings model.SiteSettings
	if err := ParseJSONStrict(r, &settings); err != nil {
		WriteBodyError(w, err)
		return
	}
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := handler.store.Update(r.Context(), settings)
	if respondMetadataMutationError(w, err) {
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update site settings")
		return
	}
	JSONResponse(w, http.StatusOK, handler.publicResponse(r.Context(), updated))
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

// publicNode 按站点展示模式渲染公共节点地址：hidden 清空，masked 只保留网络前缀，
// full 原样返回。管理端不经过这里，始终看到完整地址。
func publicNode(node model.Node, settings model.SiteSettings) model.Node {
	node.Host = model.FormatPublicAddress(node.Host, settings.PublicAddressMode)
	return node
}
