package server

import (
	"testing"
	"time"

	"beat/internal/domain"
)

func TestBuildNetworkProbeResponsePreservesUnavailableLatency(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(15 * time.Minute)
	points := []domain.NetworkProbePoint{
		{Timestamp: t0, TaskID: "task-1", TaskName: "三网巡检", Carrier: "telecom", Label: "电信目标", LossAvailable: true, LossPercent: 0, LatencyAvailable: true, LatencyAvgMs: 2, LatencyP95Ms: 3, LatencyJitterMs: 0.5},
		{Timestamp: t1, TaskID: "task-1", TaskName: "三网巡检", Carrier: "telecom", Label: "电信目标", LossAvailable: true, LossPercent: 100, LatencyAvailable: false},
		{Timestamp: t0, TaskID: "task-1", TaskName: "三网巡检", Carrier: "unicom", Label: "联通目标", LossAvailable: true, LossPercent: 100, LatencyAvailable: false},
		{Timestamp: t1, TaskID: "task-1", TaskName: "三网巡检", Carrier: "unicom", Label: "联通目标", LossAvailable: true, LossPercent: 100, LatencyAvailable: false},
	}
	definitions := []domain.TaskDefinition{{ID: "task-1", Name: "三网巡检", Kind: "three_network_probe", Enabled: true}}
	response := buildNetworkProbeResponse("node-1", t0, t1.Add(time.Minute), 15*time.Minute, points, definitions)
	if len(response.Tasks) != 1 || len(response.Tasks[0].Timestamps) != 2 || len(response.Tasks[0].Targets) != 2 {
		t.Fatalf("unexpected response: %+v", response)
	}
	task := response.Tasks[0]
	if task.TaskName != "三网巡检" || task.Targets[0].Carrier != "telecom" || task.Targets[1].Carrier != "unicom" {
		t.Fatalf("unexpected task series: %+v", task)
	}
	telecom := task.Targets[0]
	if len(telecom.LatencyAvgMs) != 2 || telecom.LatencyAvgMs[0] == nil || *telecom.LatencyAvgMs[0] != 2 || telecom.LatencyAvgMs[1] != nil {
		t.Fatalf("telecom latency must be [2, null]: %+v", telecom.LatencyAvgMs)
	}
	if telecom.LossPercent[1] == nil || *telecom.LossPercent[1] != 100 {
		t.Fatalf("telecom loss must end at 100%%: %+v", telecom.LossPercent)
	}
	unicom := task.Targets[1]
	if unicom.LatencyAvgMs[0] != nil || unicom.LatencyAvgMs[1] != nil {
		t.Fatalf("all-lost unicom latency must stay null: %+v", unicom.LatencyAvgMs)
	}
}

func TestBuildNetworkProbeResponseOnlyIncludesEnabledTasks(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	points := []domain.NetworkProbePoint{
		{Timestamp: now, TaskID: "enabled", TaskName: "旧名称", Carrier: "telecom", Label: "电信", LossAvailable: true, LatencyAvailable: true, LatencyAvgMs: 2},
		{Timestamp: now, TaskID: "disabled", TaskName: "已停用", Carrier: "telecom", Label: "电信", LossAvailable: true, LatencyAvailable: true, LatencyAvgMs: 3},
	}
	definitions := []domain.TaskDefinition{
		{ID: "enabled", Name: "已启用任务", Kind: "three_network_probe", Enabled: true},
		{ID: "disabled", Name: "已停用任务", Kind: "three_network_probe", Enabled: false},
		{ID: "waiting", Name: "等待采集任务", Kind: "three_network_probe", Enabled: true},
	}
	response := buildNetworkProbeResponse("node-1", now.Add(-time.Hour), now, time.Minute, points, definitions)
	if len(response.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2: %+v", len(response.Tasks), response.Tasks)
	}
	if response.Tasks[0].TaskID != "enabled" || response.Tasks[0].TaskName != "已启用任务" || len(response.Tasks[0].Timestamps) != 1 {
		t.Fatalf("unexpected enabled task: %+v", response.Tasks[0])
	}
	if response.Tasks[1].TaskID != "waiting" || len(response.Tasks[1].Timestamps) != 0 || len(response.Tasks[1].Targets) != 0 {
		t.Fatalf("unexpected waiting task: %+v", response.Tasks[1])
	}
	for _, task := range response.Tasks {
		if task.TaskID == "disabled" {
			t.Fatal("disabled task was returned")
		}
	}
}
