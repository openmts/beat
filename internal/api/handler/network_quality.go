package handler

import (
	"net/http"

	"github.com/beat/backend/internal/model"
)

func (h *NetworkHandler) HandlePublicNetworkQuality(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.tasks.ListTasks(r.Context(), true)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list network quality")
		return
	}
	views, err := h.buildTaskViews(r, tasks, true)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load network quality")
		return
	}
	JSONResponse(w, http.StatusOK, views)
}

func (h *NetworkHandler) HandlePublicNetworkHistory(w http.ResponseWriter, r *http.Request) {
	h.handleNetworkHistory(w, r, true)
}

func (h *NetworkHandler) HandleAdminNetworkHistory(w http.ResponseWriter, r *http.Request) {
	h.handleNetworkHistory(w, r, false)
}

func (h *NetworkHandler) handleNetworkHistory(w http.ResponseWriter, r *http.Request, public bool) {
	taskID := r.PathValue("task_id")
	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		JSONError(w, http.StatusBadRequest, "node_id is required")
		return
	}
	task, err := h.tasks.GetTask(r.Context(), taskID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read network task")
		return
	}
	if task == nil || (public && (!task.Enabled || !task.IsPublic)) {
		JSONError(w, http.StatusNotFound, "network task not found")
		return
	}
	if public {
		node, err := h.nodes.GetPublicNode(r.Context(), nodeID)
		if err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to read network node")
			return
		}
		if node == nil {
			JSONError(w, http.StatusNotFound, "network task assignment not found")
			return
		}
	}
	assigned, err := h.tasks.IsTaskAssignedToNode(r.Context(), taskID, nodeID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to verify network assignment")
		return
	}
	if !assigned {
		JSONError(w, http.StatusNotFound, "network task assignment not found")
		return
	}
	from, to, err := parseNetworkRange(r, h.now())
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	points, err := h.mts.QueryNetworkHistory(r.Context(), taskID, nodeID, from, to)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read network history")
		return
	}
	JSONResponse(w, http.StatusOK, networkHistoryResponse{
		TaskID: taskID, NodeID: nodeID, From: from, To: to, Points: points,
	})
}

func (h *NetworkHandler) buildTaskViews(
	r *http.Request,
	tasks []model.NetworkTask,
	publicOnly bool,
) ([]networkTaskView, error) {
	views := make([]networkTaskView, 0, len(tasks))
	for _, task := range tasks {
		var nodes []model.NetworkNode
		var err error
		if publicOnly {
			nodes, err = h.tasks.ListEffectivePublicTaskNodes(r.Context(), task.ID, task.AllNodes)
		} else {
			nodes, err = h.tasks.ListEffectiveTaskNodes(r.Context(), task.ID, task.AllNodes)
		}
		if err != nil {
			return nil, err
		}
		states := make([]networkNodeState, 0, len(nodes))
		for _, node := range nodes {
			latest, err := h.mts.QueryNetworkLatest(r.Context(), task.ID, node.ID)
			if err != nil {
				return nil, err
			}
			states = append(states, networkNodeState{Node: node, Latest: latest})
		}
		views = append(views, networkTaskView{Task: task, Nodes: states})
	}
	return views, nil
}
