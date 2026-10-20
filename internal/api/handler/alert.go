package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notification"
	"github.com/beat/backend/internal/store"
)

type AlertHandler struct {
	alertRuleStore    *store.AlertRuleStore
	alertChannelStore *store.AlertChannelStore
	alertEventStore   *store.AlertEventStore
	nodeStore         *store.NodeStore
	delivery          *notification.Service
	notifications     AlertNotifications
}

func NewAlertHandler(
	alertRuleStore *store.AlertRuleStore,
	alertChannelStore *store.AlertChannelStore,
	alertEventStore *store.AlertEventStore,
	nodeStore *store.NodeStore,
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
		nodeStore:         nodeStore,
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
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)

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
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)

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
	query, err := alertEventQuery(r)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid alert event query")

		return
	}

	page, err := h.alertEventStore.ListAlertEventsPage(r.Context(), query)
	if err != nil {
		if errors.Is(err, store.ErrInvalidAlertEventQuery) {
			JSONError(w, http.StatusBadRequest, "invalid alert event query")

			return
		}
		JSONError(w, http.StatusInternalServerError, "failed to list alert events")

		return
	}

	JSONResponse(w, http.StatusOK, page)
}

// alertEventQuery 解析服务端筛选与游标；非法 limit、状态或时间一律拒绝，绝不在浏览器端兜底过滤。
func alertEventQuery(request *http.Request) (store.AlertEventQuery, error) {
	values := request.URL.Query()
	query := store.AlertEventQuery{
		RuleID: values.Get("rule_id"), NodeID: values.Get("node_id"),
		Status: values.Get("status"), Cursor: values.Get("cursor"),
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return query, store.ErrInvalidAlertEventQuery
		}
		query.Limit = limit
	}
	from, err := alertEventTime(values.Get("from"))
	if err != nil {
		return query, err
	}
	to, err := alertEventTime(values.Get("to"))
	if err != nil {
		return query, err
	}
	if from != nil && to != nil && to.Before(*from) {
		return query, store.ErrInvalidAlertEventQuery
	}
	query.From, query.To = from, to
	return query, nil
}

func alertEventTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, store.ErrInvalidAlertEventQuery
	}
	utc := parsed.UTC()
	return &utc, nil
}
