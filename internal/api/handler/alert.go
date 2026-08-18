package handler

import (
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/store"
)

type AlertHandler struct {
	alertRuleStore    *store.AlertRuleStore
	alertChannelStore *store.AlertChannelStore
	alertEventStore   *store.AlertEventStore
	delivery          *notification.Service
}

func NewAlertHandler(
	alertRuleStore *store.AlertRuleStore,
	alertChannelStore *store.AlertChannelStore,
	alertEventStore *store.AlertEventStore,
	deliveryServices ...*notification.Service,
) *AlertHandler {
	delivery := notification.NewService()
	if len(deliveryServices) > 0 && deliveryServices[0] != nil {
		delivery = deliveryServices[0]
	}
	return &AlertHandler{
		alertRuleStore:    alertRuleStore,
		alertChannelStore: alertChannelStore,
		alertEventStore:   alertEventStore,
		delivery:          delivery,
	}
}

func (h *AlertHandler) HandleListAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.alertRuleStore.ListAlertRules(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list alert rules")

		return
	}

	JSONResponse(w, http.StatusOK, rules)
}

func (h *AlertHandler) HandleCreateAlertRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Metric      string  `json:"metric"`
		Operator    string  `json:"operator"`
		Threshold   float64 `json:"threshold"`
		Duration    int     `json:"duration"`
		Severity    string  `json:"severity"`
		Enabled     bool    `json:"enabled"`
	}
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	if body.Name == "" || body.Metric == "" || body.Operator == "" {
		JSONError(w, http.StatusBadRequest, "name, metric and operator are required")

		return
	}

	rule := &model.AlertRule{
		Name:        body.Name,
		Description: body.Description,
		Metric:      body.Metric,
		Operator:    body.Operator,
		Threshold:   body.Threshold,
		Duration:    body.Duration,
		Severity:    model.AlertSeverity(body.Severity),
		Enabled:     body.Enabled,
	}

	created, err := h.alertRuleStore.CreateAlertRule(r.Context(), rule)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to create alert rule")

		return
	}

	JSONResponse(w, http.StatusCreated, created)
}

func (h *AlertHandler) HandleUpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Metric      string  `json:"metric"`
		Operator    string  `json:"operator"`
		Threshold   float64 `json:"threshold"`
		Duration    int     `json:"duration"`
		Severity    string  `json:"severity"`
		Enabled     bool    `json:"enabled"`
	}
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	rule := &model.AlertRule{
		Name:        body.Name,
		Description: body.Description,
		Metric:      body.Metric,
		Operator:    body.Operator,
		Threshold:   body.Threshold,
		Duration:    body.Duration,
		Severity:    model.AlertSeverity(body.Severity),
		Enabled:     body.Enabled,
	}

	updated, err := h.alertRuleStore.UpdateAlertRule(r.Context(), id, rule)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update alert rule")

		return
	}

	JSONResponse(w, http.StatusOK, updated)
}

func (h *AlertHandler) HandleDeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.alertRuleStore.DeleteAlertRule(r.Context(), id); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to delete alert rule")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AlertHandler) HandleListAlertEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.alertEventStore.ListAlertEvents(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list alert events")

		return
	}

	JSONResponse(w, http.StatusOK, events)
}

// HandleAcknowledgeAlertEvent transitions a triggered event to acknowledged.
func (h *AlertHandler) HandleAcknowledgeAlertEvent(w http.ResponseWriter, r *http.Request) {
	event, err := h.alertEventStore.GetEventByID(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get alert event")
		return
	}
	if event == nil {
		JSONError(w, http.StatusNotFound, "alert event not found")
		return
	}
	if event.Status != model.AlertStatusTriggered {
		JSONError(w, http.StatusConflict, "only triggered events can be acknowledged")
		return
	}
	now := model.NowUTC()
	event.Status = model.AlertStatusAcknowledged
	event.AcknowledgedAt = &now
	if err := h.alertEventStore.UpdateEvent(r.Context(), event); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to acknowledge alert event")
		return
	}
	h.notifyAcknowledgment(r, event, "acknowledged")
	JSONResponse(w, http.StatusOK, event)
}

// HandleResolveAlertEvent transitions an acknowledged event to resolved.
func (h *AlertHandler) HandleResolveAlertEvent(w http.ResponseWriter, r *http.Request) {
	event, err := h.alertEventStore.GetEventByID(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get alert event")
		return
	}
	if event == nil {
		JSONError(w, http.StatusNotFound, "alert event not found")
		return
	}
	if event.Status != model.AlertStatusAcknowledged {
		JSONError(w, http.StatusConflict, "only acknowledged events can be resolved")
		return
	}
	now := model.NowUTC()
	event.Status = model.AlertStatusResolved
	event.ResolvedAt = &now
	if err := h.alertEventStore.UpdateEvent(r.Context(), event); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to resolve alert event")
		return
	}
	h.notifyAcknowledgment(r, event, "resolved")
	JSONResponse(w, http.StatusOK, event)
}

// notifyAcknowledgment pushes an acknowledgement or resolution notification to
// all enabled channels. Delivery failures are logged but do not fail the state
// change.
func (h *AlertHandler) notifyAcknowledgment(r *http.Request, event *model.AlertEvent, kind string) {
	channels, err := h.alertChannelStore.ListEnabledChannels(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "list enabled channels for alert ack", "error", err)
		return
	}
	message := "alert acknowledged"
	if kind == "resolved" {
		message = "alert resolved"
	}
	event.Message = message
	if kind == "resolved" {
		now := model.NowUTC()
		event.ResolvedAt = &now
	}
	for _, channel := range channels {
		if _, err := h.delivery.Send(r.Context(), &channel, event); err != nil {
			slog.WarnContext(r.Context(), "alert ack notification failed",
				"channel_id", channel.ID, "channel_type", channel.ChannelType, "error", err)
		}
	}
}
