package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type customMetricProbeRequest struct {
	NodeID string `json:"node_id"`
}

// customMetricProbeResponse 是试运行任务的对外快照：pending 时只有请求信息，
// 结束后带上命令输出的有界前缀与失败原因。
type customMetricProbeResponse struct {
	TaskID      string     `json:"task_id"`
	NodeID      string     `json:"node_id"`
	Status      string     `json:"status"`
	Success     *bool      `json:"success,omitempty"`
	Output      string     `json:"output,omitempty"`
	Failure     string     `json:"failure,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// HandleProbe 为某个自定义指标在指定绑定节点上排队一次试运行。
func (h *CustomMetricHandler) HandleProbe(w http.ResponseWriter, r *http.Request) {
	metricID := r.PathValue("id")
	var payload customMetricProbeRequest
	if !parseCustomMetricRequest(w, r, &payload) {
		return
	}
	task, err := h.store.QueueCustomMetricProbe(r.Context(), metricID, payload.NodeID)
	if err != nil {
		writeCustomMetricProbeError(w, err)
		return
	}
	h.dispatchTasks(r, []model.AgentTask{*task})
	JSONResponse(w, http.StatusAccepted, newCustomMetricProbeResponse(task))
}

// HandleProbeResult 返回一次试运行的终态快照，任务未结束时 status 仍为 pending。
func (h *CustomMetricHandler) HandleProbeResult(w http.ResponseWriter, r *http.Request) {
	task, err := h.store.CustomMetricProbe(r.Context(), r.PathValue("id"), r.PathValue("task_id"))
	if err != nil {
		writeCustomMetricProbeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, newCustomMetricProbeResponse(task))
}

func newCustomMetricProbeResponse(task *model.AgentTask) customMetricProbeResponse {
	response := customMetricProbeResponse{
		TaskID: task.ID, NodeID: task.NodeID, Status: task.Status,
		Output: task.Output, Failure: task.Failure, CompletedAt: task.CompletedAt,
	}
	if task.Status != model.AgentTaskStatusPending {
		success := task.Status == model.AgentTaskStatusDone
		response.Success = &success
	}
	return response
}

// dispatchTasks 与元数据变更共用同一条投递路径：已持久化的任务必须尽力推送，
// 离线节点靠重连后的重放补齐。
func (h *CustomMetricHandler) dispatchTasks(r *http.Request, tasks []model.AgentTask) {
	if h.dispatcher == nil || len(tasks) == 0 {
		return
	}
	if err := h.dispatcher.DispatchCustomMetricTasks(context.WithoutCancel(r.Context()), tasks); err != nil {
		slog.WarnContext(r.Context(), "custom metric snapshots remain pending", "error", err)
	}
}

func writeCustomMetricProbeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrCustomMetricProbeNotFound):
		JSONError(w, http.StatusNotFound, "custom metric probe not found")
	case errors.Is(err, store.ErrCustomMetricInvalid), errors.Is(err, store.ErrCustomMetricLimit):
		JSONError(w, http.StatusBadRequest, err.Error())
	default:
		JSONError(w, http.StatusInternalServerError, "custom metric probe failed")
	}
}
