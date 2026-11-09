package handler

import (
	"context"
	"net/http"
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
	targetID := r.URL.Query().Get("target_id")
	targets, ok := historyTargets(task, targetID)
	if !ok {
		JSONError(w, http.StatusBadRequest, "invalid network target")
		return
	}
	from, to, err := parseNetworkRange(r, h.now())
	if err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := historyShareKey(taskID, nodeID, targetID, from.UTC().Format(time.RFC3339Nano),
		to.UTC().Format(time.RFC3339Nano), boolKey(public))
	series, err := h.historyShare.do(r.Context(), key, func(ctx context.Context) ([]networkHistorySeries, error) {
		return h.queryHistorySeries(ctx, task, nodeID, targets, from, to, public)
	})
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read network history")
		return
	}
	JSONResponse(w, http.StatusOK, networkHistoryResponse{
		TaskID: taskID, NodeID: nodeID, From: from, To: to, Series: series,
	})
}

func (h *NetworkHandler) buildTaskViews(
	ctx context.Context,
	tasks []model.NetworkTask,
	publicOnly bool,
) ([]networkTaskView, error) {
	views := make([]networkTaskView, 0, len(tasks))
	for _, task := range tasks {
		viewTask := task
		if publicOnly {
			viewTask = publicNetworkTask(task)
		}
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
		nodeStates := make([]networkNodeState, 0, len(nodes))
		for _, node := range nodes {
			targetStates := make([]networkTargetState, 0, len(task.Targets))
			for _, target := range effectiveTargets(task) {
				latest, err := h.mts.QueryNetworkLatestForTarget(ctx, task.ID, node.ID, target.ID)
				if err != nil {
					return nil, err
				}
				targetStates = append(targetStates, networkTargetState{TargetID: target.ID, Latest: latest})
			}
			var latest *store.NetworkProbeLatest
			if len(targetStates) > 0 {
				latest = targetStates[0].Latest
			}
			nodeStates = append(nodeStates, networkNodeState{Node: node, Latest: latest, Targets: targetStates})
		}
		views = append(views, networkTaskView{Task: viewTask, Nodes: nodeStates})
	}
	return views, nil
}

func (h *NetworkHandler) queryHistorySeries(
	ctx context.Context,
	task *model.NetworkTask,
	nodeID string,
	targets []model.NetworkTarget,
	from time.Time,
	to time.Time,
	public bool,
) ([]networkHistorySeries, error) {
	series := make([]networkHistorySeries, 0, len(targets))
	for _, target := range targets {
		points, err := h.mts.QueryNetworkHistoryForTarget(ctx, task.ID, nodeID, target.ID, from, to)
		if err != nil {
			return nil, err
		}
		visible := target.VisibleAddress
		seriesTarget := target.Target
		if public && !visible {
			seriesTarget = ""
		}
		series = append(series, networkHistorySeries{
			TargetID: target.ID, Label: target.Label, Carrier: target.Carrier, Region: target.Region,
			VisibleAddress: visible, Target: seriesTarget, Points: points,
		})
	}
	return series, nil
}

func historyTargets(task *model.NetworkTask, targetID string) ([]model.NetworkTarget, bool) {
	targets := effectiveTargets(*task)
	if targetID == "" {
		return targets, true
	}
	for _, target := range targets {
		if target.ID == targetID {
			return []model.NetworkTarget{target}, true
		}
	}
	return nil, false
}

func effectiveTargets(task model.NetworkTask) []model.NetworkTarget {
	if task.Direction == model.NetworkDirectionServerToAgent {
		return []model.NetworkTarget{{
			Label: task.Name, Carrier: model.NetworkCarrierCustom, Target: task.Target,
			IPFamily: task.IPFamily, PacketCount: task.PacketCount, VisibleAddress: true,
			Enabled: true,
		}}
	}
	targets := make([]model.NetworkTarget, 0, len(task.Targets))
	for _, target := range task.Targets {
		if target.Enabled {
			targets = append(targets, target)
		}
	}
	return targets
}

func publicNetworkTask(task model.NetworkTask) model.NetworkTask {
	if task.Direction != model.NetworkDirectionAgentToTarget {
		return task
	}
	task.Target = ""
	task.Targets = append([]model.NetworkTarget(nil), task.Targets...)
	for index := range task.Targets {
		if !task.Targets[index].VisibleAddress {
			task.Targets[index].Target = ""
		}
	}
	return task
}

func boolKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
