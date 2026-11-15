package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"beat/internal/domain"
	"beat/internal/networkprobe"

	"github.com/go-chi/chi/v5"
)

func (s *Server) persistNetworkProbeResult(ctx context.Context, runID string) error {
	run, err := s.store.GetTaskRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("read completed task run: %w", err)
	}
	var result networkprobe.Result
	if err := json.Unmarshal([]byte(run.Output), &result); err != nil || result.Kind != networkprobe.Kind {
		return nil
	}
	timestamp := result.StartedAt.UTC()
	if timestamp.IsZero() && run.CompletedAt != nil {
		timestamp = run.CompletedAt.UTC()
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	points := make([]domain.NetworkProbePoint, 0, len(result.Targets))
	for _, target := range result.Targets {
		point := domain.NetworkProbePoint{
			Timestamp: timestamp, TaskID: run.TaskID, TaskName: run.TaskName,
			Carrier: target.Carrier, Label: target.Label,
			LossAvailable: target.Loss.Available,
			Sent:          target.Loss.Sent, Received: target.Loss.Received, Lost: target.Loss.Lost, Failed: target.Loss.Failed,
			LatencyAvailable: target.Latency.Available,
		}
		if target.Loss.Percent != nil {
			point.LossPercent = *target.Loss.Percent
		}
		if target.Latency.MinMs != nil {
			point.LatencyMinMs = *target.Latency.MinMs
		}
		if target.Latency.AvgMs != nil {
			point.LatencyAvgMs = *target.Latency.AvgMs
		}
		if target.Latency.MaxMs != nil {
			point.LatencyMaxMs = *target.Latency.MaxMs
		}
		if target.Latency.P50Ms != nil {
			point.LatencyP50Ms = *target.Latency.P50Ms
		}
		if target.Latency.P95Ms != nil {
			point.LatencyP95Ms = *target.Latency.P95Ms
		}
		if target.Latency.JitterMs != nil {
			point.LatencyJitterMs = *target.Latency.JitterMs
		}
		points = append(points, point)
	}
	return s.tsdb.AppendNetworkProbes(ctx, run.NodeID, points)
}

func (s *Server) handleNodeNetworkProbes(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	window, step := parseRange(r.URL.Query().Get("range"), now)
	nodeID := chi.URLParam(r, "id")
	if _, err := s.store.GetNode(r.Context(), nodeID, now); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "节点不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取节点失败")
		return
	}
	points, err := s.tsdb.QueryNetworkProbes(r.Context(), nodeID, now.Add(-window), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取三网质量时序失败")
		return
	}
	definitions, err := s.store.ListTaskDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "读取三网任务配置失败")
		return
	}
	enabled := make([]domain.TaskDefinition, 0)
	for _, definition := range definitions {
		if definition.Kind == networkprobe.Kind && definition.Enabled {
			enabled = append(enabled, definition)
		}
	}
	writeJSON(w, http.StatusOK, buildNetworkProbeResponse(nodeID, now.Add(-window), now, step, points, enabled))
}

type networkProbeBucketAggregate struct {
	timestamp          time.Time
	lossSum            float64
	lossCount          int
	latencyAvgSum      float64
	latencyAvgCount    int
	latencyP95Sum      float64
	latencyP95Count    int
	latencyJitterSum   float64
	latencyJitterCount int
}

type networkProbeCarrierAggregate struct {
	carrier string
	label   string
	buckets map[int64]*networkProbeBucketAggregate
}

type networkProbeTaskAggregate struct {
	taskID   string
	taskName string
	lastRun  time.Time
	carriers map[string]*networkProbeCarrierAggregate
}

func buildNetworkProbeResponse(nodeID string, from, to time.Time, step time.Duration, points []domain.NetworkProbePoint, definitions []domain.TaskDefinition) domain.NetworkProbeResponse {
	response := domain.NetworkProbeResponse{
		NodeID: nodeID, From: from, To: to, StepSeconds: int(step.Seconds()),
		Tasks: make([]domain.NetworkProbeTaskSeries, 0),
	}
	if step <= 0 {
		step = time.Minute
	}
	stepSeconds := int64(step.Seconds())
	enabledTasks := make(map[string]domain.TaskDefinition, len(definitions))
	for _, definition := range definitions {
		if definition.Kind == networkprobe.Kind && definition.Enabled {
			enabledTasks[definition.ID] = definition
		}
	}
	tasks := make(map[string]*networkProbeTaskAggregate)
	for _, point := range points {
		definition, enabled := enabledTasks[point.TaskID]
		if !enabled {
			continue
		}
		task := tasks[point.TaskID]
		if task == nil {
			task = &networkProbeTaskAggregate{taskID: point.TaskID, carriers: make(map[string]*networkProbeCarrierAggregate)}
			tasks[point.TaskID] = task
		}
		if definition.Name != "" {
			task.taskName = definition.Name
		} else if point.TaskName != "" {
			task.taskName = point.TaskName
		}
		if point.Timestamp.After(task.lastRun) {
			task.lastRun = point.Timestamp
		}
		carrier := task.carriers[point.Carrier]
		if carrier == nil {
			carrier = &networkProbeCarrierAggregate{carrier: point.Carrier, buckets: make(map[int64]*networkProbeBucketAggregate)}
			task.carriers[point.Carrier] = carrier
		}
		if point.Label != "" {
			carrier.label = point.Label
		}
		bucketKey := point.Timestamp.Unix() - point.Timestamp.Unix()%stepSeconds
		bucket := carrier.buckets[bucketKey]
		if bucket == nil {
			bucket = &networkProbeBucketAggregate{timestamp: time.Unix(bucketKey, 0).UTC()}
			carrier.buckets[bucketKey] = bucket
		}
		if point.LossAvailable {
			bucket.lossSum += point.LossPercent
			bucket.lossCount++
		}
		if point.LatencyAvailable {
			bucket.latencyAvgSum += point.LatencyAvgMs
			bucket.latencyAvgCount++
			bucket.latencyP95Sum += point.LatencyP95Ms
			bucket.latencyP95Count++
			bucket.latencyJitterSum += point.LatencyJitterMs
			bucket.latencyJitterCount++
		}
	}

	taskIDs := make([]string, 0, len(tasks))
	for taskID := range tasks {
		taskIDs = append(taskIDs, taskID)
	}
	sort.Slice(taskIDs, func(left, right int) bool {
		return tasks[taskIDs[left]].lastRun.After(tasks[taskIDs[right]].lastRun)
	})
	for _, taskID := range taskIDs {
		task := tasks[taskID]
		bucketSet := make(map[int64]time.Time)
		carriers := make([]*networkProbeCarrierAggregate, 0, len(task.carriers))
		for _, carrier := range task.carriers {
			carriers = append(carriers, carrier)
			for key, bucket := range carrier.buckets {
				bucketSet[key] = bucket.timestamp
			}
		}
		sort.Slice(carriers, func(left, right int) bool {
			return carrierRank(carriers[left].carrier) < carrierRank(carriers[right].carrier)
		})
		keys := make([]int64, 0, len(bucketSet))
		for key := range bucketSet {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })
		timestamps := make([]time.Time, 0, len(keys))
		for _, key := range keys {
			timestamps = append(timestamps, bucketSet[key])
		}
		targets := make([]domain.NetworkProbeTargetSeries, 0, len(carriers))
		for _, carrier := range carriers {
			target := domain.NetworkProbeTargetSeries{
				Carrier: carrier.carrier, Label: carrier.label,
				LossPercent: make([]*float64, len(keys)), LatencyAvgMs: make([]*float64, len(keys)),
				LatencyP95Ms: make([]*float64, len(keys)), LatencyJitterMs: make([]*float64, len(keys)),
			}
			for index, key := range keys {
				bucket := carrier.buckets[key]
				if bucket == nil {
					continue
				}
				if bucket.lossCount > 0 {
					target.LossPercent[index] = floatPointer(bucket.lossSum / float64(bucket.lossCount))
				}
				if bucket.latencyAvgCount > 0 {
					target.LatencyAvgMs[index] = floatPointer(bucket.latencyAvgSum / float64(bucket.latencyAvgCount))
				}
				if bucket.latencyP95Count > 0 {
					target.LatencyP95Ms[index] = floatPointer(bucket.latencyP95Sum / float64(bucket.latencyP95Count))
				}
				if bucket.latencyJitterCount > 0 {
					target.LatencyJitterMs[index] = floatPointer(bucket.latencyJitterSum / float64(bucket.latencyJitterCount))
				}
			}
			targets = append(targets, target)
		}
		taskName := task.taskName
		if taskName == "" {
			taskName = task.taskID
		}
		response.Tasks = append(response.Tasks, domain.NetworkProbeTaskSeries{
			TaskID: task.taskID, TaskName: taskName, LastRunAt: task.lastRun, Timestamps: timestamps, Targets: targets,
		})
	}
	for _, definition := range definitions {
		if !definition.Enabled || definition.Kind != networkprobe.Kind {
			continue
		}
		if _, exists := tasks[definition.ID]; exists {
			continue
		}
		response.Tasks = append(response.Tasks, domain.NetworkProbeTaskSeries{
			TaskID: definition.ID, TaskName: definition.Name, Timestamps: []time.Time{}, Targets: []domain.NetworkProbeTargetSeries{},
		})
	}
	sort.SliceStable(response.Tasks, func(left, right int) bool {
		leftTask, rightTask := response.Tasks[left], response.Tasks[right]
		if leftTask.LastRunAt.IsZero() != rightTask.LastRunAt.IsZero() {
			return !leftTask.LastRunAt.IsZero()
		}
		if !leftTask.LastRunAt.Equal(rightTask.LastRunAt) {
			return leftTask.LastRunAt.After(rightTask.LastRunAt)
		}
		return leftTask.TaskName < rightTask.TaskName
	})
	return response
}

func carrierRank(carrier string) int {
	switch carrier {
	case networkprobe.CarrierTelecom:
		return 0
	case networkprobe.CarrierUnicom:
		return 1
	case networkprobe.CarrierMobile:
		return 2
	default:
		return 3
	}
}

func floatPointer(value float64) *float64 {
	return &value
}
