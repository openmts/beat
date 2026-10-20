package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type evaluationWindowReference struct {
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	Aggregation  string    `json:"aggregation"`
	RuleRevision int64     `json:"rule_revision"`
}

type evaluationNotificationReference struct {
	SourceKey string                     `json:"source_key"`
	Status    string                     `json:"status"`
	Receipt   *notificationqueue.Receipt `json:"receipt,omitempty"`
}

type evaluationDetail struct {
	State              *model.AlertEvaluationState       `json:"state"`
	Rule               *model.AlertEvaluationRule        `json:"rule"`
	Window             *evaluationWindowReference        `json:"window"`
	RecentHealthEvents []model.AlertEvaluationEvent      `json:"recent_health_events"`
	Notifications      []evaluationNotificationReference `json:"notifications"`
}

func (h *AlertEvaluationHandler) HandleGetEvaluation(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	if _, ok := alertEventActor(w, r); !ok {
		return
	}
	key := store.AlertEvaluationKey{RuleID: r.PathValue("rule_id"), NodeID: r.PathValue("node_id"), DeviceID: r.URL.Query().Get("device_id")}
	if key.DeviceID != "" && !model.ValidAcceleratorDeviceID(key.DeviceID) {
		evaluationError(w, alerteval.ErrInvalidPolicy)
		return
	}
	detail, err := h.evaluationDetail(r.Context(), key)
	if err != nil {
		evaluationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, detail)
}

func (h *AlertEvaluationHandler) evaluationDetail(ctx context.Context, key store.AlertEvaluationKey) (evaluationDetail, error) {
	detail := evaluationDetail{Notifications: []evaluationNotificationReference{}}
	state, err := h.config.States.Get(ctx, key)
	if err != nil {
		return detail, err
	}
	if state == nil {
		return detail, store.ErrAlertEvaluationRuleNotFound
	}
	detail.State = state
	detail.Rule, err = h.config.Rules.Get(ctx, key.RuleID)
	if err != nil && !errors.Is(err, store.ErrAlertEvaluationRuleNotFound) {
		return detail, err
	}
	if detail.Rule != nil && detail.Rule.Revision == state.RuleRevision && state.LastEvidenceAt != nil {
		detail.Window = &evaluationWindowReference{From: state.LastEvidenceAt.Add(-time.Duration(detail.Rule.WindowSeconds) * time.Second), To: *state.LastEvidenceAt, Aggregation: detail.Rule.Aggregation, RuleRevision: state.RuleRevision}
	}
	events, err := h.config.States.ListEvents(ctx, store.AlertEvaluationQuery{RuleID: key.RuleID, NodeID: key.NodeID, DeviceID: key.DeviceID, DeviceScoped: true, Limit: 3})
	if err != nil {
		return detail, err
	}
	detail.RecentHealthEvents = events.Items
	sources := evaluationDetailSources(state, events.Items)
	for _, source := range sources {
		reference, err := h.evaluationReceipt(ctx, source)
		if err != nil {
			return detail, err
		}
		detail.Notifications = append(detail.Notifications, reference)
	}
	return detail, nil
}

func evaluationDetailSources(state *model.AlertEvaluationState, events []model.AlertEvaluationEvent) []string {
	sources := []string{}
	if state.ActiveEventID != "" {
		sources = append(sources, "alert:"+state.ActiveEventID+":triggered")
		if state.ActiveEventStatus == model.AlertStatusAcknowledged {
			sources = append(sources, "alert:"+state.ActiveEventID+":acknowledged")
		}
	}
	for _, event := range events {
		if event.NotifiedAt != nil {
			sources = append(sources, event.SourceKey)
		}
	}
	return sources
}

func (h *AlertEvaluationHandler) evaluationReceipt(ctx context.Context, source string) (evaluationNotificationReference, error) {
	reference := evaluationNotificationReference{SourceKey: source, Status: "history_unavailable"}
	if h.config.Receipts == nil {
		return reference, alerteval.ErrUnavailable
	}
	receipt, err := h.config.Receipts.SourceReceipt(ctx, source)
	if errors.Is(err, notificationqueue.ErrNotFound) {
		return reference, nil
	}
	if err != nil {
		return reference, err
	}
	reference.Status, reference.Receipt = receipt.State, &receipt
	return reference, nil
}
