package handler

import (
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
)

type alertChannelRequest struct {
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
	Config      string `json:"config"`
	Enabled     bool   `json:"enabled"`
}

func (h *AlertHandler) HandleListAlertChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.alertChannelStore.ListAlertChannels(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list alert channels")
		return
	}
	for index := range channels {
		h.prepareAlertChannel(&channels[index])
	}
	JSONResponse(w, http.StatusOK, channels)
}

func (h *AlertHandler) HandleCreateAlertChannel(w http.ResponseWriter, r *http.Request) {
	var body alertChannelRequest
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Name == "" || body.ChannelType == "" {
		JSONError(w, http.StatusBadRequest, "name and channel_type are required")
		return
	}
	normalized, err := notification.NormalizeConfig(body.ChannelType, body.Config, "")
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	channel := &model.AlertChannel{
		Name: body.Name, ChannelType: body.ChannelType, Config: normalized, Enabled: body.Enabled,
	}
	created, err := h.alertChannelStore.CreateAlertChannel(r.Context(), channel)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to create alert channel")
		return
	}
	h.prepareAlertChannel(created)
	JSONResponse(w, http.StatusCreated, created)
}

func (h *AlertHandler) HandleUpdateAlertChannel(w http.ResponseWriter, r *http.Request) {
	body, existing, ok := h.parseAlertChannelUpdate(w, r)
	if !ok {
		return
	}
	normalized, err := notification.NormalizeConfig(
		body.ChannelType,
		body.Config,
		matchingChannelConfig(existing, body.ChannelType),
	)
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	channel := &model.AlertChannel{
		Name: body.Name, ChannelType: body.ChannelType, Config: normalized,
		Enabled: body.Enabled, CreatedAt: existing.CreatedAt,
	}
	updated, err := h.alertChannelStore.UpdateAlertChannel(r.Context(), existing.ID, channel)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update alert channel")
		return
	}
	h.prepareAlertChannel(updated)
	JSONResponse(w, http.StatusOK, updated)
}

func (h *AlertHandler) parseAlertChannelUpdate(
	w http.ResponseWriter,
	r *http.Request,
) (alertChannelRequest, *model.AlertChannel, bool) {
	var body alertChannelRequest
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return body, nil, false
	}
	if body.Name == "" || body.ChannelType == "" {
		JSONError(w, http.StatusBadRequest, "name and channel_type are required")
		return body, nil, false
	}
	existing, err := h.alertChannelStore.GetAlertChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get alert channel")
		return body, nil, false
	}
	if existing == nil {
		JSONError(w, http.StatusNotFound, "alert channel not found")
		return body, nil, false
	}
	return body, existing, true
}

func (h *AlertHandler) HandleDeleteAlertChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.alertChannelStore.DeleteAlertChannel(r.Context(), id); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to delete alert channel")
		return
	}
	h.delivery.Forget(id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AlertHandler) HandleTestAlertChannel(w http.ResponseWriter, r *http.Request) {
	channel, err := h.alertChannelStore.GetAlertChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get alert channel")
		return
	}
	if channel == nil {
		JSONError(w, http.StatusNotFound, "alert channel not found")
		return
	}
	event := &model.AlertEvent{
		ID: "test", RuleID: "test", NodeID: "test", Message: "Beat test notification",
		Status: model.AlertStatusTriggered, TriggeredAt: model.NowUTC(),
	}
	status, _ := h.delivery.Send(r.Context(), channel, event)
	JSONResponse(w, http.StatusOK, status)
}

func (h *AlertHandler) prepareAlertChannel(channel *model.AlertChannel) {
	channel.LastDelivery = h.delivery.Status(channel.ID)
	channel.Config = notification.SanitizeConfig(channel.ChannelType, channel.Config)
}

func matchingChannelConfig(channel *model.AlertChannel, channelType string) string {
	if channel.ChannelType == channelType {
		return channel.Config
	}
	return ""
}
