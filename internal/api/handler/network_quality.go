package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// HandlePublicNetworkQuality 复用上一代公共网络质量视图：同一代内的观看者共享一次构建，
// 写入之后立即失效，过期后由下一个请求重建。
func (h *NetworkHandler) HandlePublicNetworkQuality(w http.ResponseWriter, r *http.Request) {
	view, err := h.sharedPublicQualityView(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load network quality")
		return
	}
	if view.failure != "" {
		JSONError(w, http.StatusInternalServerError, view.failure)
		return
	}
	writeNetworkQualityView(w, r, view.snapshot)
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
	key := historyShareKey(taskID, nodeID, from.UTC().Format(time.RFC3339Nano),
		to.UTC().Format(time.RFC3339Nano), strconv.FormatBool(public))
	points, err := h.historyShare.do(r.Context(), key, func(ctx context.Context) ([]store.NetworkProbePoint, error) {
		return h.mts.QueryNetworkHistory(ctx, taskID, nodeID, from, to)
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read network history")
		return
	}
	JSONResponse(w, http.StatusOK, networkHistoryResponse{
		TaskID: taskID, NodeID: nodeID, From: from, To: to, Points: points,
	})
}

func (h *NetworkHandler) buildTaskViews(
	ctx context.Context,
	tasks []model.NetworkTask,
	publicOnly bool,
) ([]networkTaskView, error) {
	views := make([]networkTaskView, 0, len(tasks))
	for _, task := range tasks {
		var nodes []model.NetworkNode
		var err error
		if publicOnly {
			nodes, err = h.tasks.ListEffectivePublicTaskNodes(ctx, task.ID, task.AllNodes)
		} else {
			nodes, err = h.tasks.ListEffectiveTaskNodes(ctx, task.ID, task.AllNodes)
		}
		if err != nil {
			return nil, err
		}
		states := make([]networkNodeState, 0, len(nodes))
		for _, node := range nodes {
			latest, err := h.mts.QueryNetworkLatest(ctx, task.ID, node.ID)
			if err != nil {
				return nil, err
			}
			states = append(states, networkNodeState{Node: node, Latest: latest})
		}
		views = append(views, networkTaskView{Task: task, Nodes: states})
	}
	return views, nil
}
