package handler

import (
	"errors"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/notificationqueue"
)

type NotificationHandler struct{ queue *notificationqueue.Service }

func NewNotificationHandler(queue *notificationqueue.Service) *NotificationHandler {
	return &NotificationHandler{queue: queue}
}

func notificationActor(w http.ResponseWriter, r *http.Request) (notificationqueue.Actor, bool) {
	w.Header().Set("Cache-Control", "no-store")
	principal, ok := middleware.AdminPrincipal(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return notificationqueue.Actor{}, false
	}
	return notificationqueue.Actor{UserID: principal.User.ID, SessionID: principal.Session.ID, RequestID: middleware.RequestID(r.Context()), IPAddress: middleware.ClientIP(r), UserAgent: r.UserAgent()}, true
}

func notificationError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, "notification operation is unavailable"
	switch {
	case errors.Is(err, notificationqueue.ErrNotFound):
		status, message = http.StatusNotFound, "notification record not found"
	case errors.Is(err, notificationqueue.ErrConflict):
		status, message = http.StatusConflict, "notification state changed; refresh and retry"
	case errors.Is(err, notificationqueue.ErrForbidden):
		status, message = http.StatusForbidden, "notification operation is not authorized"
	case errors.Is(err, notificationqueue.ErrRecentAuth):
		status, message = http.StatusPreconditionRequired, "recent authentication is required"
	case errors.Is(err, notificationqueue.ErrRateLimited):
		status, message = http.StatusTooManyRequests, "notification operation budget exceeded"
	case errors.Is(err, notificationqueue.ErrInvalidRequest), errors.Is(err, notification.ErrProviderConfig), errors.Is(err, notification.ErrTemplate):
		status, message = http.StatusBadRequest, "notification configuration or request is invalid"
	case errors.Is(err, notificationqueue.ErrQueueCapacity):
		status, message = http.StatusServiceUnavailable, "notification queue capacity reached"
	}
	JSONError(w, status, message)
}

func (handler *NotificationHandler) HandleListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := handler.queue.ListChannels(r.Context())
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, channels)
}

func (handler *NotificationHandler) HandleGetChannel(w http.ResponseWriter, r *http.Request) {
	channel, err := handler.queue.GetChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, channel)
}

func (handler *NotificationHandler) HandleCreateChannel(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	var body model.NotificationChannelWrite
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	channel, err := handler.queue.CreateChannel(r.Context(), actor, body)
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, channel)
}

func (handler *NotificationHandler) HandleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	var body model.NotificationChannelWrite
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	channel, err := handler.queue.UpdateChannel(r.Context(), actor, notificationqueue.ChannelUpdate{ID: r.PathValue("id"), Write: body})
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, channel)
}

func (handler *NotificationHandler) HandleSetEnabled(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	channel, err := handler.queue.SetEnabled(r.Context(), actor, notificationqueue.ChannelEnabled{ID: r.PathValue("id"), Enabled: body.Enabled})
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, channel)
}

func (handler *NotificationHandler) HandleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	if err := handler.queue.DeleteChannel(r.Context(), actor, r.PathValue("id")); err != nil {
		notificationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *NotificationHandler) HandleTestChannel(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	receipt, err := handler.queue.TestChannel(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusAccepted, receipt)
}

func (handler *NotificationHandler) HandlePreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := notificationActor(w, r)
	if !ok {
		return
	}
	var body notificationqueue.PreviewWrite
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	preview, err := handler.queue.Preview(r.Context(), actor, body)
	if err != nil {
		notificationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, preview)
}
