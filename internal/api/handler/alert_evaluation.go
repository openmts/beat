package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
)

type AlertEvaluationPreviewer interface {
	Preview(context.Context, string, model.AlertEvaluationPreviewRequest) ([]model.AlertEvaluationPreview, error)
}

type AlertEvaluationReceipts interface {
	SourceReceipt(context.Context, string) (notificationqueue.Receipt, error)
}

type AlertEvaluationHandlerConfig struct {
	Rules    *store.AlertEvaluationRuleStore
	States   *store.AlertEvaluationStore
	Preview  AlertEvaluationPreviewer
	Receipts AlertEvaluationReceipts
}

type AlertEvaluationHandler struct{ config AlertEvaluationHandlerConfig }

func NewAlertEvaluationHandler(config AlertEvaluationHandlerConfig) *AlertEvaluationHandler {
	return &AlertEvaluationHandler{config: config}
}

func evaluationHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func evaluationError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "storage_unavailable"
	switch {
	case errors.Is(err, alerteval.ErrInvalidPolicy):
		status, code = http.StatusBadRequest, "invalid_policy"
	case errors.Is(err, store.ErrAlertEvaluationRuleNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, alerteval.ErrConflict):
		status, code = http.StatusConflict, "stale_revision"
	case errors.Is(err, alerteval.ErrPolicyInUse):
		status, code = http.StatusConflict, "active_incident_policy"
	case errors.Is(err, store.ErrAdminSessionNotFound):
		status, code = http.StatusUnauthorized, "session_required"
	case errors.Is(err, alerteval.ErrCapacity):
		status, code = http.StatusTooManyRequests, "capacity"
		w.Header().Set("Retry-After", "30")
	}
	evaluationHeaders(w)
	JSONError(w, status, code)
}

func decodeEvaluationBody(r *http.Request, value any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, alerteval.MaximumRequestBytes+1))
	if err != nil || len(data) > alerteval.MaximumRequestBytes {
		return alerteval.ErrInvalidPolicy
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return alerteval.ErrInvalidPolicy
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return alerteval.ErrInvalidPolicy
	}
	return nil
}

func evaluationReadQuery(r *http.Request) (store.AlertEvaluationQuery, error) {
	values := r.URL.Query()
	query := store.AlertEvaluationQuery{RuleID: values.Get("rule_id"), NodeID: values.Get("node_id"), DeviceID: values.Get("device_id"), DeviceScoped: values.Has("device_id"), Phase: values.Get("phase"), Status: values.Get("status"), Kind: values.Get("kind"), Cursor: values.Get("cursor")}
	if len(query.RuleID) > 128 || len(query.NodeID) > 128 {
		return query, alerteval.ErrInvalidPolicy
	}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return query, alerteval.ErrInvalidPolicy
		}
		query.Limit = limit
	}
	return query, nil
}

func (h *AlertEvaluationHandler) HandleListEvaluations(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	if _, ok := alertEventActor(w, r); !ok {
		return
	}
	query, err := evaluationReadQuery(r)
	if err != nil {
		evaluationError(w, err)
		return
	}
	page, err := h.config.States.List(r.Context(), query)
	if err != nil {
		evaluationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, page)
}

func (h *AlertEvaluationHandler) HandleListEvaluationEvents(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	if _, ok := alertEventActor(w, r); !ok {
		return
	}
	query, err := evaluationReadQuery(r)
	if err != nil {
		evaluationError(w, err)
		return
	}
	page, err := h.config.States.ListEvents(r.Context(), query)
	if err != nil {
		evaluationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, page)
}
