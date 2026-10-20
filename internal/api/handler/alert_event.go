package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type AlertNotifications interface {
	Transition(context.Context, store.AlertEventTransition) (notificationqueue.Receipt, error)
}

type AlertEventChangeResult struct {
	model.AlertEvent
	Notification notificationqueue.Receipt `json:"notification"`
}

func (handler *AlertHandler) SetNotifications(notifications AlertNotifications) {
	handler.notifications = notifications
}

func (handler *AlertHandler) HandleAcknowledgeAlertEvent(w http.ResponseWriter, request *http.Request) {
	handler.changeEvent(w, request, model.AlertStatusAcknowledged)
}

func (handler *AlertHandler) HandleResolveAlertEvent(w http.ResponseWriter, request *http.Request) {
	handler.changeEvent(w, request, model.AlertStatusResolved)
}

func (handler *AlertHandler) changeEvent(w http.ResponseWriter, request *http.Request, target model.AlertStatus) {
	actor, ok := alertEventActor(w, request)
	if !ok {
		return
	}
	event, err := handler.alertEventStore.GetEventByID(request.Context(), request.PathValue("id"))
	if err != nil {
		respondEventError(w, err)
		return
	}
	if event == nil {
		JSONError(w, http.StatusNotFound, "alert event not found")
		return
	}
	change, err := prepareEventTransition(*event, target)
	if err != nil {
		respondEventError(w, err)
		return
	}
	if handler.notifications == nil {
		respondEventError(w, notificationqueue.ErrQueueUnavailable)
		return
	}
	if err := handler.enrichEvent(request.Context(), &change.Event); err != nil {
		respondEventError(w, err)
		return
	}
	change.Actor = &actor
	change.Event.Message = eventTransitionMessage(change.Event)
	receipt, err := handler.notifications.Transition(request.Context(), change)
	if err != nil {
		respondEventError(w, err)
		return
	}
	if receipt.Skipped || receipt.Duplicate {
		respondEventError(w, store.ErrInvalidAlertTransition)
		return
	}
	JSONResponse(w, http.StatusOK, AlertEventChangeResult{AlertEvent: change.Event, Notification: receipt})
}

func prepareEventTransition(event model.AlertEvent, target model.AlertStatus) (store.AlertEventTransition, error) {
	change := store.AlertEventTransition{Previous: event.Status, Event: event}
	now := model.NowUTC()
	switch {
	case event.Status == model.AlertStatusTriggered && target == model.AlertStatusAcknowledged:
		change.Event.AcknowledgedAt = &now
	case event.Status == model.AlertStatusAcknowledged && target == model.AlertStatusResolved:
		change.Event.ResolvedAt = &now
	default:
		return change, store.ErrInvalidAlertTransition
	}
	change.Event.Status = target
	return change, nil
}

func alertEventActor(w http.ResponseWriter, request *http.Request) (store.EventMutationActor, bool) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok || principal.User.ID == "" || principal.Session.ID == "" {
		JSONError(w, http.StatusUnauthorized, "administrator session is required")
		return store.EventMutationActor{}, false
	}
	return store.EventMutationActor{UserID: principal.User.ID, SessionID: principal.Session.ID,
		RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request), UserAgent: request.UserAgent()}, true
}

func (handler *AlertHandler) enrichEvent(ctx context.Context, event *model.AlertEvent) error {
	rule, err := handler.alertRuleStore.GetRuleByID(ctx, event.RuleID)
	if err != nil {
		return err
	}
	if rule != nil {
		event.RuleName, event.Metric, event.Severity = rule.Name, rule.Metric, rule.Severity
	}
	if handler.nodeStore == nil {
		return nil
	}
	node, err := handler.nodeStore.GetNode(ctx, event.NodeID)
	if err != nil {
		return err
	}
	if node != nil {
		event.NodeName = node.Name
	}
	return nil
}

func eventTransitionMessage(event model.AlertEvent) string {
	nodeName, ruleName := event.NodeName, event.RuleName
	if nodeName == "" {
		nodeName = event.NodeID
	}
	if ruleName == "" {
		ruleName = event.RuleID
	}
	return fmt.Sprintf("alert %s: %s on node %s, value=%.2f", event.Status, ruleName, nodeName, event.Value)
}

func respondEventError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidAlertTransition):
		JSONError(w, http.StatusConflict, "alert event state changed; refresh before retrying")
	case errors.Is(err, store.ErrAdminSessionNotFound):
		JSONError(w, http.StatusUnauthorized, "administrator session is no longer active")
	case errors.Is(err, notificationqueue.ErrQueueUnavailable), errors.Is(err, notificationqueue.ErrQueueCapacity):
		w.Header().Set("Retry-After", "5")
		JSONError(w, http.StatusServiceUnavailable, "notification queue unavailable; event was not changed")
	default:
		JSONError(w, http.StatusInternalServerError, "failed to update alert event")
	}
}
