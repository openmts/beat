package handler

import (
	"net/http"

	"github.com/beat/backend/internal/alerteval"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type evaluationRuleWrite struct {
	model.AlertEvaluationRule
	ExpectedRevision int64 `json:"expected_revision"`
}

func (h *AlertEvaluationHandler) HandleListEvaluationRules(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	if _, ok := alertEventActor(w, r); !ok {
		return
	}
	rules, err := h.config.Rules.List(r.Context())
	if err != nil {
		evaluationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, rules)
}

func (h *AlertEvaluationHandler) HandleGetEvaluationRule(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	if _, ok := alertEventActor(w, r); !ok {
		return
	}
	rule, err := h.config.Rules.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		evaluationError(w, err)
		return
	}
	if rule.DeletedAt != nil {
		evaluationError(w, store.ErrAlertEvaluationRuleNotFound)
		return
	}
	JSONResponse(w, http.StatusOK, rule)
}

func (h *AlertEvaluationHandler) HandleSaveEvaluationRule(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	actor, ok := alertEventActor(w, r)
	if !ok {
		return
	}
	var body evaluationRuleWrite
	if err := decodeEvaluationBody(r, &body); err != nil {
		evaluationError(w, err)
		return
	}
	change := store.AlertRuleMutation{Rule: body.AlertEvaluationRule, ExpectedRevision: body.ExpectedRevision, Actor: actor}
	var rule *model.AlertEvaluationRule
	var err error
	status := http.StatusCreated
	if id := r.PathValue("id"); id != "" {
		if body.ExpectedRevision < 1 {
			evaluationError(w, alerteval.ErrInvalidPolicy)
			return
		}
		rule, err = h.config.Rules.Update(r.Context(), id, change)
		status = http.StatusOK
	} else {
		rule, err = h.config.Rules.Create(r.Context(), change)
	}
	if err != nil {
		evaluationError(w, err)
		return
	}
	JSONResponse(w, status, rule)
}

func (h *AlertEvaluationHandler) HandleDeleteEvaluationRule(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	actor, ok := alertEventActor(w, r)
	if !ok {
		return
	}
	var body struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeEvaluationBody(r, &body); err != nil || body.ExpectedRevision < 1 {
		evaluationError(w, alerteval.ErrInvalidPolicy)
		return
	}
	if err := h.config.Rules.Delete(r.Context(), r.PathValue("id"), store.AlertRuleMutation{ExpectedRevision: body.ExpectedRevision, Actor: actor}); err != nil {
		evaluationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AlertEvaluationHandler) HandlePreviewEvaluationRule(w http.ResponseWriter, r *http.Request) {
	evaluationHeaders(w)
	actor, ok := alertEventActor(w, r)
	if !ok {
		return
	}
	var body model.AlertEvaluationPreviewRequest
	if err := decodeEvaluationBody(r, &body); err != nil {
		evaluationError(w, err)
		return
	}
	if h.config.Preview == nil {
		evaluationError(w, alerteval.ErrUnavailable)
		return
	}
	result, err := h.config.Preview.Preview(r.Context(), actor.UserID, body)
	if err != nil {
		evaluationError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, result)
}
