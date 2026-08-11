package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (h *NetworkHandler) HandleNetworkAssignments(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	assignments, err := h.tasks.ListAssignments(r.Context(), identity.NodeID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list network assignments")
		return
	}
	JSONResponse(w, http.StatusOK, struct {
		ExpiresAt time.Time                 `json:"expires_at"`
		Tasks     []model.NetworkAssignment `json:"tasks"`
	}{ExpiresAt: h.now().Add(networkAssignmentTTL), Tasks: assignments})
}

func (h *NetworkHandler) HandleNetworkResults(w http.ResponseWriter, r *http.Request) {
	identity, ok := middleware.AgentIdentity(r.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxNetworkResultBody)
	var body struct {
		NodeName string                     `json:"node_name"`
		Results  []model.NetworkProbeResult `json:"results"`
	}
	if err := ParseJSON(r, &body); err != nil || len(body.Results) == 0 || len(body.Results) > 64 {
		JSONError(w, http.StatusBadRequest, "invalid network result batch")
		return
	}
	samples, status, err := h.validateNetworkResults(r, identity.NodeID, body.Results)
	if err != nil {
		JSONError(w, status, err.Error())
		return
	}
	if err := h.mts.WriteNetworkProbes(r.Context(), samples); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to store network results")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *NetworkHandler) validateNetworkResults(
	r *http.Request,
	nodeID string,
	results []model.NetworkProbeResult,
) ([]store.NetworkProbeSample, int, error) {
	now := h.now()
	samples := make([]store.NetworkProbeSample, 0, len(results))
	for _, result := range results {
		if err := result.Validate(); err != nil || result.FinishedAt.Before(now.Add(-5*time.Minute)) ||
			result.FinishedAt.After(now.Add(5*time.Minute)) {
			return nil, http.StatusBadRequest, errors.New("invalid network result")
		}
		assigned, err := h.tasks.GetAssignedTask(r.Context(), nodeID, result.TaskID)
		if err != nil {
			return nil, http.StatusInternalServerError, errors.New("failed to verify network result")
		}
		if assigned == nil || !assigned.Enabled || !assigned.Assigned {
			return nil, http.StatusConflict, errors.New("network task is not assigned")
		}
		samples = append(samples, store.NetworkProbeSample{
			TaskID: result.TaskID, NodeID: assigned.NodeID, TaskType: assigned.TaskType,
			FinishedAt: result.FinishedAt.UTC(), LatencyMS: result.LatencyMS,
			Success: result.Success, StatusCode: result.StatusCode, ErrorCode: result.ErrorCode,
		})
	}
	return samples, http.StatusNoContent, nil
}
