package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"beat/internal/networkprobe"
	"beat/internal/platform/sqlite"
	"beat/internal/taskmetrics"

	"github.com/go-chi/chi/v5"
)

type taskSummary struct {
	Total    int `json:"total"`
	Enabled  int `json:"enabled"`
	Runs     int `json:"runs"`
	Failures int `json:"failures"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTaskDefinitions(r.Context())
	if err != nil {
		s.logger.Error("list_task_definitions_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取任务定义失败")
		return
	}
	summary := taskSummary{Total: len(tasks)}
	for _, task := range tasks {
		if task.Enabled {
			summary.Enabled++
		}
		summary.Runs += task.RunCount
		summary.Failures += task.FailureCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   tasks,
		"total":   len(tasks),
		"summary": summary,
	})
}

const taskKindShell = "shell_script"

type taskRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Kind            string          `json:"kind"`
	Script          string          `json:"script"`
	Config          json.RawMessage `json:"config"`
	IntervalSeconds int             `json:"intervalSeconds"`
	TimeoutSeconds  int             `json:"timeoutSeconds"`
	Enabled         bool            `json:"enabled"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input taskRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", "任务配置格式无效")
		return
	}
	if input.IntervalSeconds == 0 {
		input.IntervalSeconds = 300
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 30
	}
	if strings.TrimSpace(input.Kind) == "" {
		input.Kind = taskKindShell
	}
	kind, name, description, script, config, err := normalizeTaskDefinition(input.Kind, input.Name, input.Description, input.Script, input.Config, input.IntervalSeconds, input.TimeoutSeconds)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	task, err := s.store.CreateTask(r.Context(), kind, name, description, script, config, input.IntervalSeconds, input.TimeoutSeconds, input.Enabled, time.Now().UTC())
	if err != nil {
		s.logger.Error("create_custom_task_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "创建任务失败")
		return
	}
	s.logger.Info("custom_task_created", "task_id", task.ID, "name", task.Name, "enabled", task.Enabled)
	writeJSON(w, http.StatusCreated, task)
}

type taskUpdateRequest struct {
	Name            *string         `json:"name"`
	Description     *string         `json:"description"`
	Kind            *string         `json:"kind"`
	Script          *string         `json:"script"`
	Config          json.RawMessage `json:"config"`
	IntervalSeconds *int            `json:"intervalSeconds"`
	TimeoutSeconds  *int            `json:"timeoutSeconds"`
	Enabled         *bool           `json:"enabled"`
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var input taskUpdateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", "任务配置格式无效")
		return
	}
	task, err := s.store.GetTaskDefinition(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sqlite.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "任务不存在")
		return
	}
	if err != nil {
		s.logger.Error("get_task_definition_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取任务失败")
		return
	}
	if input.Name != nil {
		task.Name = *input.Name
	}
	if input.Description != nil {
		task.Description = *input.Description
	}
	if input.Kind != nil && strings.TrimSpace(*input.Kind) != "" && *input.Kind != task.Kind {
		writeError(w, http.StatusBadRequest, "invalid_task", "任务类型创建后不能修改")
		return
	}
	if input.Script != nil {
		task.Script = *input.Script
	}
	if len(input.Config) > 0 {
		task.Config = input.Config
	}
	if input.IntervalSeconds != nil {
		task.IntervalSeconds = *input.IntervalSeconds
	}
	if input.TimeoutSeconds != nil {
		task.TimeoutSeconds = *input.TimeoutSeconds
	}
	if input.Enabled != nil {
		task.Enabled = *input.Enabled
	}
	task.Kind, task.Name, task.Description, task.Script, task.Config, err = normalizeTaskDefinition(task.Kind, task.Name, task.Description, task.Script, task.Config, task.IntervalSeconds, task.TimeoutSeconds)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_task", err.Error())
		return
	}
	updated, err := s.store.UpdateTaskDefinition(r.Context(), task, time.Now().UTC())
	if errors.Is(err, sqlite.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "任务不存在")
		return
	}
	if err != nil {
		s.logger.Error("update_task_definition_failed", "error", err, "task_id", task.ID)
		writeError(w, http.StatusInternalServerError, "storage_error", "保存任务失败")
		return
	}
	s.logger.Info("task_definition_updated", "task_id", updated.ID, "enabled", updated.Enabled)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := s.store.DeleteCustomTask(r.Context(), id)
	switch {
	case errors.Is(err, sqlite.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, "not_found", "任务不存在")
	case err != nil:
		s.logger.Error("delete_custom_task_failed", "error", err, "task_id", id)
		writeError(w, http.StatusInternalServerError, "storage_error", "删除任务失败")
	default:
		s.logger.Info("custom_task_deleted", "task_id", id)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
	}
}

func (s *Server) handleTaskRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	runs, err := s.store.ListTaskRuns(r.Context(), r.URL.Query().Get("taskId"), limit)
	if err != nil {
		s.logger.Error("list_task_runs_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取任务执行记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs, "total": len(runs)})
}

func (s *Server) handleTaskRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetTaskRun(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, sqlite.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "任务执行记录不存在")
		return
	}
	if err != nil {
		s.logger.Error("get_task_run_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "storage_error", "读取任务执行记录失败")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func normalizeTaskDefinition(kind, name, description, script string, config json.RawMessage, intervalSeconds, timeoutSeconds int) (string, string, string, string, json.RawMessage, error) {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	script = strings.TrimSpace(script)
	if name == "" || len([]rune(name)) > 64 {
		return "", "", "", "", nil, errors.New("任务名称不能为空且不能超过 64 个字符")
	}
	if len([]rune(description)) > 500 {
		return "", "", "", "", nil, errors.New("任务描述不能超过 500 个字符")
	}
	if intervalSeconds < 30 || intervalSeconds > 7*24*60*60 {
		return "", "", "", "", nil, errors.New("执行周期必须在 30 秒到 7 天之间")
	}
	if timeoutSeconds < 1 || timeoutSeconds > 300 {
		return "", "", "", "", nil, errors.New("执行超时必须在 1 到 300 秒之间")
	}
	switch kind {
	case taskKindShell:
		if script == "" {
			return "", "", "", "", nil, errors.New("任务脚本不能为空")
		}
		if len(script) > 16<<10 {
			return "", "", "", "", nil, errors.New("任务脚本不能超过 16 KiB")
		}
		metricConfig, err := taskmetrics.ParseConfig(config)
		if err != nil {
			return "", "", "", "", nil, err
		}
		normalized, err := taskmetrics.MarshalConfig(metricConfig)
		if err != nil {
			return "", "", "", "", nil, err
		}
		return kind, name, description, script, normalized, nil
	case networkprobe.Kind:
		if len(config) == 0 {
			return "", "", "", "", nil, errors.New("三网探测配置不能为空")
		}
		probeConfig, err := networkprobe.ParseConfig(config)
		if err != nil {
			return "", "", "", "", nil, err
		}
		minimumTimeoutSeconds := (probeConfig.Probe.Count*(probeConfig.Probe.IntervalMs+probeConfig.Probe.TimeoutMs) + 999) / 1000
		if timeoutSeconds < minimumTimeoutSeconds {
			return "", "", "", "", nil, fmt.Errorf("任务超时至少需要 %d 秒，才能覆盖当前 TCP 探测次数与超时", minimumTimeoutSeconds)
		}
		normalized, err := networkprobe.MarshalConfig(probeConfig)
		if err != nil {
			return "", "", "", "", nil, err
		}
		return kind, name, description, "", normalized, nil
	default:
		return "", "", "", "", nil, errors.New("不支持的任务类型")
	}
}
