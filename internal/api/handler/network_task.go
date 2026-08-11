package handler

import (
	"net/http"
)

func (h *NetworkHandler) HandleListNetworkTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.tasks.ListTasks(r.Context(), false)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list network tasks")
		return
	}
	views, err := h.buildTaskViews(r, tasks, false)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load network task state")
		return
	}
	JSONResponse(w, http.StatusOK, views)
}

func (h *NetworkHandler) HandleCreateNetworkTask(w http.ResponseWriter, r *http.Request) {
	var body networkTaskRequest
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	task, err := h.tasks.CreateTask(r.Context(), body.task(), body.NodeIDs)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid network task")
		return
	}
	JSONResponse(w, http.StatusCreated, task)
}

func (h *NetworkHandler) HandleUpdateNetworkTask(w http.ResponseWriter, r *http.Request) {
	var body networkTaskRequest
	if err := ParseJSON(r, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	task, err := h.tasks.UpdateTask(r.Context(), r.PathValue("task_id"), body.task(), body.NodeIDs)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid network task")
		return
	}
	if task == nil {
		JSONError(w, http.StatusNotFound, "network task not found")
		return
	}
	JSONResponse(w, http.StatusOK, task)
}

func (h *NetworkHandler) HandleDeleteNetworkTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	task, err := h.tasks.GetTask(r.Context(), taskID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read network task")
		return
	}
	if task == nil {
		JSONError(w, http.StatusNotFound, "network task not found")
		return
	}
	if err := h.mts.DeleteNetworkTask(r.Context(), taskID); err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to delete network task history")
		return
	}
	deleted, err := h.tasks.DeleteTask(r.Context(), taskID)
	if err != nil || !deleted {
		JSONError(w, http.StatusInternalServerError, "failed to delete network task")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *NetworkHandler) HandleSortNetworkTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := ParseJSON(r, &body); err != nil || len(body.IDs) == 0 {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.tasks.UpdateSortOrder(r.Context(), body.IDs); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid network task order")
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}
