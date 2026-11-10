package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const networkResultMaxAge = 5 * time.Minute

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
	if err := ParseJSON(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	if len(body.Results) == 0 || len(body.Results) > 64 {
		JSONError(w, http.StatusBadRequest, "invalid network result batch")
		return
	}
	now := h.now()
	fresh, expired := splitExpiredNetworkResults(body.Results, now)
	if len(fresh) == 0 && len(expired) > 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	samples, status, err := h.validateNetworkResults(r, identity.NodeID, fresh, now)
	if err != nil {
		JSONError(w, status, err.Error())
		return
	}
	if err := h.mts.WriteNetworkProbes(r.Context(), samples); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to store network results")
		return
	}
	h.invalidateNetworkQuality()
	w.WriteHeader(http.StatusNoContent)
}

func (h *NetworkHandler) validateNetworkResults(
	r *http.Request,
	nodeID string,
	results []model.NetworkProbeResult,
	now time.Time,
) ([]store.NetworkProbeSample, int, error) {
	samples := make([]store.NetworkProbeSample, 0, len(results))
	for _, result := range results {
		if err := result.Validate(); err != nil || result.TargetID == "" ||
			result.FinishedAt.After(now.Add(networkResultMaxAge)) {
			return nil, http.StatusBadRequest, errors.New("invalid network result")
		}
		assigned, err := h.tasks.GetAssignedTarget(r.Context(), nodeID, result.TaskID, result.TargetID)
		if err != nil {
			return nil, http.StatusInternalServerError, errors.New("failed to verify network result")
		}
		if assigned == nil {
			return nil, http.StatusBadRequest, errors.New("invalid network result")
		}
		if !assigned.Enabled || !assigned.Assigned ||
			assigned.Direction != model.NetworkDirectionAgentToTarget {
			return nil, http.StatusConflict, errors.New("network task is not assigned")
		}
		samples = append(samples, store.NetworkProbeSample{
			TaskID: result.TaskID, TargetID: assigned.TargetID, NodeID: assigned.NodeID,
			TaskType: assigned.TaskType, Direction: assigned.Direction,
			Carrier: assigned.Carrier, Region: assigned.Region, IPFamily: assigned.IPFamily,
			FinishedAt: result.FinishedAt.UTC(), LatencyMS: result.LatencyMS,
			Success: result.Success, StatusCode: result.StatusCode, ErrorCode: result.ErrorCode,
			SentPackets: result.SentPackets, ReceivedPackets: result.ReceivedPackets,
			AttemptCount: result.AttemptCount, SuccessCount: result.SuccessCount,
			SuccessPercent: result.SuccessPercent,
			LossPercent:    result.LossPercent, JitterMS: result.JitterMS,
			MinLatencyMS: result.MinLatencyMS, MaxLatencyMS: result.MaxLatencyMS,
			P95LatencyMS: result.P95LatencyMS, StdDevLatencyMS: result.StdDevLatencyMS,
		})
	}
	return samples, http.StatusNoContent, nil
}

// splitExpiredNetworkResults 丢弃超过服务端接收窗口的结果。旧版 Agent 在
// 整批永久失败时不会清空队列，继续返回 400 会让它永远阻塞；确认该批次而不
// 落库可让客户端丢弃过期数据并重新上报。
func splitExpiredNetworkResults(
	results []model.NetworkProbeResult,
	now time.Time,
) (fresh, expired []model.NetworkProbeResult) {
	cutoff := now.Add(-networkResultMaxAge)
	fresh = make([]model.NetworkProbeResult, 0, len(results))
	for _, result := range results {
		if !result.FinishedAt.IsZero() && result.FinishedAt.Before(cutoff) {
			expired = append(expired, result)
			continue
		}
		fresh = append(fresh, result)
	}
	return fresh, expired
}
