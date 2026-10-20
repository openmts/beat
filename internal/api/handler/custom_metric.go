package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// CustomMetricDispatcher dispatches registration tasks to agents when custom
// metric bindings change.
type CustomMetricDispatcher interface {
	DispatchCustomMetricTasks(context.Context, []model.AgentTask) error
}

type CustomMetricHandler struct {
	store      *store.CustomMetricStore
	dispatcher CustomMetricDispatcher
	samples    customSampleSource
}

func NewCustomMetricHandler(store *store.CustomMetricStore, dispatcher CustomMetricDispatcher) *CustomMetricHandler {
	return &CustomMetricHandler{store: store, dispatcher: dispatcher}
}

type customMetricPayload struct {
	Name            string `json:"name"`
	ShellCommand    string `json:"shell_command"`
	IntervalSeconds int    `json:"interval_seconds"`
	Description     string `json:"description"`
	Enabled         *bool  `json:"enabled"`
}

func (h *CustomMetricHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	metrics, err := h.store.List(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list custom metrics")
		return
	}
	JSONResponse(w, http.StatusOK, metrics)
}

func (h *CustomMetricHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var payload customMetricPayload
	if !parseCustomMetricRequest(w, r, &payload) {
		return
	}
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	created, err := h.store.Create(r.Context(), model.CustomMetric{
		Name:            payload.Name,
		ShellCommand:    payload.ShellCommand,
		IntervalSeconds: payload.IntervalSeconds,
		Description:     payload.Description,
		Enabled:         enabled,
	})
	if err != nil {
		writeCustomMetricError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, created)
}

func (h *CustomMetricHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := h.store.Get(r.Context(), id)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get custom metric")
		return
	}
	if existing == nil {
		JSONError(w, http.StatusNotFound, "custom metric not found")
		return
	}
	var payload customMetricPayload
	if !parseCustomMetricRequest(w, r, &payload) {
		return
	}
	updated := *existing
	updated.Name = payload.Name
	updated.ShellCommand = payload.ShellCommand
	updated.IntervalSeconds = payload.IntervalSeconds
	updated.Description = payload.Description
	if payload.Enabled != nil {
		updated.Enabled = *payload.Enabled
	}
	change, err := h.store.UpdateWithRegistration(r.Context(), id, updated)
	if err != nil {
		writeCustomMetricError(w, err)
		return
	}
	if change.Metric == nil {
		JSONError(w, http.StatusNotFound, "custom metric not found")
		return
	}
	h.dispatchChange(r, change)
	JSONResponse(w, http.StatusOK, change.Metric)
}

func (h *CustomMetricHandler) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	change, err := h.store.DeleteWithRegistration(r.Context(), id)
	if err != nil {
		writeCustomMetricError(w, err)
		return
	}
	h.dispatchChange(r, change)
	JSONResponse(w, http.StatusOK, struct{}{})
}

// HandleListMetricNodes returns the node IDs a custom metric is bound to.
func (h *CustomMetricHandler) HandleListMetricNodes(w http.ResponseWriter, r *http.Request) {
	metricID := r.PathValue("id")
	ids, err := h.store.NodeIDsForMetric(r.Context(), metricID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list custom metric nodes")
		return
	}
	JSONResponse(w, http.StatusOK, ids)
}

// HandleSetMetricNodes replaces the nodes bound to a custom metric and
// dispatches registration tasks to the affected nodes.
func (h *CustomMetricHandler) HandleSetMetricNodes(w http.ResponseWriter, r *http.Request) {
	metricID := r.PathValue("id")
	var payload struct {
		NodeIDs []string `json:"node_ids"`
	}
	if !parseCustomMetricRequest(w, r, &payload) {
		return
	}
	change, err := h.store.SetMetricNodesWithRegistration(r.Context(), metricID, payload.NodeIDs)
	if err != nil {
		writeCustomMetricError(w, err)
		return
	}
	h.dispatchChange(r, change)
	JSONResponse(w, http.StatusOK, payload.NodeIDs)
}

func (h *CustomMetricHandler) dispatchChange(r *http.Request, change store.CustomMetricChange) {
	h.dispatchTasks(r, change.Tasks)
}

func writeCustomMetricError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrCustomMetricNameTaken) {
		JSONError(w, http.StatusConflict, "custom metric name already exists")
		return
	}
	if errors.Is(err, store.ErrCustomMetricInvalid) || errors.Is(err, store.ErrCustomMetricLimit) {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	JSONError(w, http.StatusInternalServerError, "custom metric change failed")
}
