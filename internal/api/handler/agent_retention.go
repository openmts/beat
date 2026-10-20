package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/retentionadmin"
)

// AgentRetentionOperations 是发布对象保留期的后台读写面。
type AgentRetentionOperations interface {
	Get(context.Context) (model.AgentRetentionSettings, error)
	Update(context.Context, model.AgentRetentionSettings) (model.AgentRetentionSettings, error)
	Reset(context.Context) (model.AgentRetentionSettings, error)
	Run(context.Context) (model.AgentRetentionSettings, error)
}

// AgentRetentionHandler 暴露「发布对象保留期」的后台配置与手动清理。
type AgentRetentionHandler struct {
	operations AgentRetentionOperations
}

func NewAgentRetentionHandler(operations AgentRetentionOperations) *AgentRetentionHandler {
	return &AgentRetentionHandler{operations: operations}
}

// HandleGet 返回显式配置、环境变量缺省与最近一轮清理结果。
func (handler *AgentRetentionHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	settings, err := handler.operations.Get(r.Context())
	if err != nil {
		writeAgentRetentionError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

type agentRetentionInput struct {
	RolloutDays int `json:"rollout_days"`
	ReleaseDays int `json:"release_days"`
}

// HandleUpdate 保存管理员配置；0 天表示保留全部历史。
func (handler *AgentRetentionHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var input agentRetentionInput
	if err := ParseJSONStrict(r, &input); err != nil {
		WriteBodyError(w, err)
		return
	}
	settings, err := handler.operations.Update(r.Context(), model.AgentRetentionSettings{
		RolloutDays: input.RolloutDays, ReleaseDays: input.ReleaseDays})
	if err != nil {
		writeAgentRetentionError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

// HandleReset 丢弃后台配置，回到启动环境变量缺省。
func (handler *AgentRetentionHandler) HandleReset(w http.ResponseWriter, r *http.Request) {
	settings, err := handler.operations.Reset(r.Context())
	if err != nil {
		writeAgentRetentionError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

// HandleRun 立即执行一轮清理，便于管理员验证配置是否按预期生效。
func (handler *AgentRetentionHandler) HandleRun(w http.ResponseWriter, r *http.Request) {
	settings, err := handler.operations.Run(r.Context())
	if err != nil {
		writeAgentRetentionError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, settings)
}

func writeAgentRetentionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrAgentRetentionInvalid):
		JSONError(w, http.StatusBadRequest, "retention days must be between 0 and 3650")
	case errors.Is(err, retentionadmin.ErrDisabled):
		JSONError(w, http.StatusConflict, "agent release retention is disabled; set a retention period first")
	default:
		JSONError(w, http.StatusServiceUnavailable, "agent release retention is unavailable")
	}
}
