package handler

import (
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
)

func (handler *NotificationHandler) HandleListDeliveries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := notificationqueue.DeliveryFilter{State: query.Get("state"), ChannelID: query.Get("channel_id")}
	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			notificationError(w, notificationqueue.ErrInvalidRequest)
			return
		}
		filter.Limit = limit
	}
	if value := query.Get("offset"); value != "" {
		offset, err := strconv.Atoi(value)
		if err != nil {
			notificationError(w, notificationqueue.ErrInvalidRequest)
			return
		}
		filter.Offset = offset
	}
	page, err := handler.queue.ListDeliveries(r.Context(), filter)
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, page)
}

func (handler *NotificationHandler) HandleGetDelivery(w http.ResponseWriter, r *http.Request) {
	delivery, err := handler.queue.GetDelivery(r.Context(), r.PathValue("id"))
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, delivery)
}

func (handler *NotificationHandler) HandleRetry(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	var body struct {
		Confirm bool `json:"confirm_duplicate"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	delivery, err := handler.queue.RetryDelivery(r.Context(), actor, notificationqueue.RetryRequest{ID: r.PathValue("id"), ConfirmDuplicate: body.Confirm})
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusAccepted, delivery)
}

func (handler *NotificationHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	if err := handler.queue.CancelDelivery(r.Context(), actor, r.PathValue("id")); err != nil {
		notificationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *NotificationHandler) HandleSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := handler.queue.Settings(r.Context())
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

func (handler *NotificationHandler) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	var body model.NotificationSettingsWrite
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	settings, err := handler.queue.UpdateSettings(r.Context(), actor, body)
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

func (handler *NotificationHandler) HandleEventKinds(w http.ResponseWriter, _ *http.Request) {
	items := []map[string]any{}
	for _, kind := range []string{"alert.triggered", "alert.acknowledged", "alert.resolved", "availability.offline", "availability.recovered", "traffic.quota", "traffic.report", "fleet.status_summary", "notification.delivery_failed", "operation.completed", "operation.failed", "alert.no_data", "alert.evaluation_error", "alert.evaluation_recovered"} {
		items = append(items, map[string]any{"kind": kind, "available": true})
	}
	JSONResponse(w, http.StatusOK, map[string]any{"items": items})
}
