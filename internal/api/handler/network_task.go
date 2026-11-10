package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func (h *NetworkHandler) HandleListNetworkTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.tasks.ListTasks(r.Context(), false)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list network tasks")
		return
	}
	views, err := h.buildTaskViews(r.Context(), tasks, false)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load network task state")
		return
	}
	JSONResponse(w, http.StatusOK, views)
}

func (h *NetworkHandler) HandleCreateNetworkTask(w http.ResponseWriter, r *http.Request) {
	var body networkTaskRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	taskInput := body.task()
	if err := validateNetworkTaskRequest(&taskInput, body.NodeIDs); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := h.tasks.CreateTask(r.Context(), taskInput, body.NodeIDs)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to create network task")
		return
	}
	h.invalidateNetworkQuality()
	JSONResponse(w, http.StatusCreated, task)
}

func (h *NetworkHandler) HandleUpdateNetworkTask(w http.ResponseWriter, r *http.Request) {
	var body networkTaskRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	taskInput := body.task()
	if err := validateNetworkTaskRequest(&taskInput, body.NodeIDs); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := h.tasks.GetTask(r.Context(), r.PathValue("task_id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load network task")
		return
	}
	if existing == nil {
		JSONError(w, http.StatusNotFound, "network task not found")
		return
	}
	if existing.Direction != taskInput.Direction {
		JSONError(w, http.StatusBadRequest, "network task direction cannot be changed")
		return
	}
	task, err := h.tasks.UpdateTask(r.Context(), r.PathValue("task_id"), taskInput, body.NodeIDs)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update network task")
		return
	}
	if task == nil {
		JSONError(w, http.StatusNotFound, "network task not found")
		return
	}
	h.invalidateNetworkQuality()
	JSONResponse(w, http.StatusOK, task)
}

// validateNetworkTaskRequest 在进入存储层前给出可执行的校验原因，避免管理界面
// 只收到笼统的 "invalid network task"。
func validateNetworkTaskRequest(task *model.NetworkTask, nodeIDs []string) error {
	task.Normalize()
	if err := task.Validate(); err != nil {
		return err
	}
	if !task.AllNodes && len(nodeIDs) == 0 {
		return errors.New("explicit network task assignment requires at least one node")
	}
	seen := make(map[string]struct{}, len(nodeIDs))
	for _, id := range nodeIDs {
		if strings.TrimSpace(id) == "" {
			return errors.New("network task node ID is required")
		}
		if _, exists := seen[id]; exists {
			return errors.New("network task node IDs must be unique")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// WithErasureStore 注入耐久擦除账本；任务删除因此先发布 tombstone 再删除应用行。
func (h *NetworkHandler) WithErasureStore(jobs *store.MetricErasureStore) *NetworkHandler {
	h.erasure = jobs
	return h
}

func (h *NetworkHandler) HandleDeleteNetworkTask(w http.ResponseWriter, r *http.Request) {
	view, replayed, ok := createDeletionJob(w, r, h.erasure, model.MetricErasureDeleteNetworkTask,
		r.PathValue("task_id"), model.NowUTC())
	if !ok {
		return
	}
	h.invalidateNetworkQuality()
	JSONResponse(w, http.StatusAccepted, deletionAcceptedResponse{Job: view, Replayed: replayed})
}

func (h *NetworkHandler) HandleSortNetworkTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	if len(body.IDs) == 0 {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.tasks.UpdateSortOrder(r.Context(), body.IDs); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid network task order")
		return
	}
	h.invalidateNetworkQuality()
	JSONResponse(w, http.StatusOK, struct{}{})
}
