package tsdb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"beat/internal/domain"

	mts "github.com/openmts/mts"
)

const (
	measurement             = "node_metrics"
	networkProbeMeasurement = "network_probe"
)

type MTS struct {
	engine *mts.Engine
	path   string
	sync   bool
}

func OpenMTS(ctx context.Context, path string, retention time.Duration, syncWrites bool, logger *slog.Logger) (*MTS, error) {
	opts := mts.DefaultOptions(path)
	opts.Retention = retention
	opts.WAL.Sync = syncWrites
	opts.FlushSync = syncWrites
	opts.Compaction.Enabled = true
	opts.Compaction.BackgroundInterval = 30 * time.Second
	opts.Compaction.MaxConcurrent = 2
	opts.Logger = logger
	engine, err := mts.Open(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("open mts: %w", err)
	}
	return &MTS{engine: engine, path: path, sync: syncWrites}, nil
}

func (m *MTS) Append(ctx context.Context, nodeID string, metrics domain.Metrics) error {
	return m.AppendBatch(ctx, nodeID, []domain.Metrics{metrics})
}

func (m *MTS) AppendBatch(ctx context.Context, nodeID string, points []domain.Metrics) error {
	if len(points) == 0 {
		return nil
	}
	items := make([]mts.Point, 0, len(points))
	for _, point := range points {
		items = append(items, mts.Point{
			Measurement: measurement,
			Tags:        map[string]string{"node_id": nodeID},
			Timestamp:   point.Timestamp.UnixNano(),
			Fields: map[string]mts.FieldValue{
				"cpu_usage":          mts.Float64Value(point.CPUUsage),
				"cpu_user":           mts.Float64Value(point.CPUUser),
				"cpu_system":         mts.Float64Value(point.CPUSystem),
				"cpu_iowait":         mts.Float64Value(point.CPUIowait),
				"memory_used_bytes":  mts.Float64Value(float64(point.MemoryUsedBytes)),
				"memory_total_bytes": mts.Float64Value(float64(point.MemoryTotalBytes)),
				"memory_usage":       mts.Float64Value(point.MemoryUsage),
				"swap_used_bytes":    mts.Float64Value(float64(point.SwapUsedBytes)),
				"disk_used_bytes":    mts.Float64Value(float64(point.DiskUsedBytes)),
				"disk_total_bytes":   mts.Float64Value(float64(point.DiskTotalBytes)),
				"disk_usage":         mts.Float64Value(point.DiskUsage),
				"network_rx_bps":     mts.Float64Value(point.NetworkRxBps),
				"network_tx_bps":     mts.Float64Value(point.NetworkTxBps),
				"load1":              mts.Float64Value(point.Load1),
				"load5":              mts.Float64Value(point.Load5),
				"load15":             mts.Float64Value(point.Load15),
				"processes":          mts.Float64Value(float64(point.Processes)),
				"tcp_connections":    mts.Float64Value(float64(point.TCPConnections)),
				"uptime_seconds":     mts.Float64Value(float64(point.UptimeSeconds)),
			},
		})
	}
	if err := m.engine.WritePointsAsTypedBatch(ctx, items, mts.WriteOptions{Sync: m.sync}); err != nil {
		return fmt.Errorf("write mts points: %w", err)
	}
	return nil
}

func (m *MTS) Query(ctx context.Context, nodeID string, from, to time.Time) ([]domain.Metrics, error) {
	query, err := mts.NewQuery().
		From("", "", measurement).
		Where(mts.TagEq("node_id", nodeID)).
		TimeRangeTime(from, to).
		OrderByTimeAsc().
		Limit(100000).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build mts query: %w", err)
	}
	rows, err := m.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query mts: %w", err)
	}
	points := make([]domain.Metrics, 0, len(rows))
	for _, row := range rows {
		points = append(points, metricsFromRow(row))
	}
	return points, nil
}

func (m *MTS) QueryAll(ctx context.Context, from, to time.Time) (map[string][]domain.Metrics, error) {
	query, err := mts.NewQuery().
		From("", "", measurement).
		TimeRangeTime(from, to).
		OrderByTimeAsc().
		Limit(200000).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build mts query: %w", err)
	}
	rows, err := m.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query mts: %w", err)
	}
	result := make(map[string][]domain.Metrics)
	for _, row := range rows {
		nodeID := row.Tags["node_id"]
		if nodeID == "" {
			continue
		}
		result[nodeID] = append(result[nodeID], metricsFromRow(row))
	}
	return result, nil
}

func (m *MTS) AppendNetworkProbes(ctx context.Context, nodeID string, points []domain.NetworkProbePoint) error {
	if len(points) == 0 {
		return nil
	}
	items := make([]mts.Point, 0, len(points))
	for _, point := range points {
		items = append(items, mts.Point{
			Measurement: networkProbeMeasurement,
			Tags: map[string]string{
				"node_id": nodeID, "task_id": point.TaskID, "task_name": point.TaskName,
				"carrier": point.Carrier, "label": point.Label,
			},
			Timestamp: point.Timestamp.UnixNano(),
			Fields: map[string]mts.FieldValue{
				"loss_available":    mts.Float64Value(boolFloat(point.LossAvailable)),
				"loss_percent":      mts.Float64Value(point.LossPercent),
				"sent":              mts.Float64Value(float64(point.Sent)),
				"received":          mts.Float64Value(float64(point.Received)),
				"lost":              mts.Float64Value(float64(point.Lost)),
				"failed":            mts.Float64Value(float64(point.Failed)),
				"latency_available": mts.Float64Value(boolFloat(point.LatencyAvailable)),
				"latency_min_ms":    mts.Float64Value(point.LatencyMinMs),
				"latency_avg_ms":    mts.Float64Value(point.LatencyAvgMs),
				"latency_max_ms":    mts.Float64Value(point.LatencyMaxMs),
				"latency_p50_ms":    mts.Float64Value(point.LatencyP50Ms),
				"latency_p95_ms":    mts.Float64Value(point.LatencyP95Ms),
				"latency_jitter_ms": mts.Float64Value(point.LatencyJitterMs),
			},
		})
	}
	if err := m.engine.WritePointsAsTypedBatch(ctx, items, mts.WriteOptions{Sync: m.sync}); err != nil {
		return fmt.Errorf("write network probe points: %w", err)
	}
	return nil
}

func (m *MTS) QueryNetworkProbes(ctx context.Context, nodeID string, from, to time.Time) ([]domain.NetworkProbePoint, error) {
	query, err := mts.NewQuery().
		From("", "", networkProbeMeasurement).
		Where(mts.TagEq("node_id", nodeID)).
		TimeRangeTime(from, to).
		OrderByTimeAsc().
		Limit(100000).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build network probe query: %w", err)
	}
	rows, err := m.engine.QueryRows(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query network probes: %w", err)
	}
	points := make([]domain.NetworkProbePoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, domain.NetworkProbePoint{
			Timestamp:        time.Unix(0, row.Timestamp).UTC(),
			TaskID:           row.Tags["task_id"],
			TaskName:         row.Tags["task_name"],
			Carrier:          row.Tags["carrier"],
			Label:            row.Tags["label"],
			LossAvailable:    floatField(row.Fields, "loss_available") != 0,
			LossPercent:      floatField(row.Fields, "loss_percent"),
			Sent:             int(floatField(row.Fields, "sent")),
			Received:         int(floatField(row.Fields, "received")),
			Lost:             int(floatField(row.Fields, "lost")),
			Failed:           int(floatField(row.Fields, "failed")),
			LatencyAvailable: floatField(row.Fields, "latency_available") != 0,
			LatencyMinMs:     floatField(row.Fields, "latency_min_ms"),
			LatencyAvgMs:     floatField(row.Fields, "latency_avg_ms"),
			LatencyMaxMs:     floatField(row.Fields, "latency_max_ms"),
			LatencyP50Ms:     floatField(row.Fields, "latency_p50_ms"),
			LatencyP95Ms:     floatField(row.Fields, "latency_p95_ms"),
			LatencyJitterMs:  floatField(row.Fields, "latency_jitter_ms"),
		})
	}
	return points, nil
}

func (m *MTS) AppendTaskMetrics(ctx context.Context, nodeID, measurement, taskID, taskName string, timestamp time.Time, metrics map[string]float64) error {
	if len(metrics) == 0 {
		return nil
	}
	fields := make(map[string]mts.FieldValue, len(metrics))
	for key, value := range metrics {
		fields[key] = mts.Float64Value(value)
	}
	point := mts.Point{
		Measurement: measurement,
		Tags: map[string]string{
			"node_id": nodeID, "task_id": taskID, "task_name": taskName,
		},
		Timestamp: timestamp.UTC().UnixNano(),
		Fields:    fields,
	}
	if err := m.engine.WritePointsAsTypedBatch(ctx, []mts.Point{point}, mts.WriteOptions{Sync: m.sync}); err != nil {
		return fmt.Errorf("write task metrics to %s: %w", measurement, err)
	}
	return nil
}

func (m *MTS) DeleteNode(ctx context.Context, nodeID string) error {
	for _, name := range []string{measurement, networkProbeMeasurement} {
		if err := m.engine.Delete(ctx, mts.DeleteRequest{
			Measurement: name,
			Tags:        map[string]string{"node_id": nodeID},
			StartTime:   0,
			EndTime:     time.Now().Add(time.Minute).UnixNano(),
		}); err != nil {
			return fmt.Errorf("delete mts node data from %s: %w", name, err)
		}
	}
	return nil
}

func (m *MTS) Stats(ctx context.Context) (Stats, error) {
	health := m.engine.HealthSnapshot()
	measurements, err := m.engine.ListMeasurements(ctx, "default")
	if err != nil {
		return Stats{}, err
	}
	sizeBytes, err := directorySize(m.path)
	if err != nil {
		return Stats{}, err
	}
	checks := make([]HealthCheck, 0, len(health.Checks))
	for _, check := range health.Checks {
		checks = append(checks, HealthCheck{Name: check.Name, Status: check.Status, Reason: check.Reason})
	}
	reasons := health.Reasons
	if len(reasons) > 20 {
		reasons = append([]string(nil), reasons[:20]...)
	}
	return Stats{
		Driver:       "mts",
		Path:         m.path,
		Healthy:      health.Healthy,
		Ready:        health.Ready,
		Measurements: len(measurements),
		SizeBytes:    sizeBytes,
		Reasons:      reasons,
		Checks:       checks,
	}, nil
}

func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("scan MTS data size: %w", err)
	}
	return total, nil
}

func (m *MTS) Close(ctx context.Context) error {
	return m.engine.Close(ctx)
}

func metricsFromRow(row mts.Row) domain.Metrics {
	return domain.Metrics{
		Timestamp:        time.Unix(0, row.Timestamp).UTC(),
		CPUUsage:         floatField(row.Fields, "cpu_usage"),
		CPUUser:          floatField(row.Fields, "cpu_user"),
		CPUSystem:        floatField(row.Fields, "cpu_system"),
		CPUIowait:        floatField(row.Fields, "cpu_iowait"),
		MemoryUsedBytes:  uint64(floatField(row.Fields, "memory_used_bytes")),
		MemoryTotalBytes: uint64(floatField(row.Fields, "memory_total_bytes")),
		MemoryUsage:      floatField(row.Fields, "memory_usage"),
		SwapUsedBytes:    uint64(floatField(row.Fields, "swap_used_bytes")),
		DiskUsedBytes:    uint64(floatField(row.Fields, "disk_used_bytes")),
		DiskTotalBytes:   uint64(floatField(row.Fields, "disk_total_bytes")),
		DiskUsage:        floatField(row.Fields, "disk_usage"),
		NetworkRxBps:     floatField(row.Fields, "network_rx_bps"),
		NetworkTxBps:     floatField(row.Fields, "network_tx_bps"),
		Load1:            floatField(row.Fields, "load1"),
		Load5:            floatField(row.Fields, "load5"),
		Load15:           floatField(row.Fields, "load15"),
		Processes:        int(floatField(row.Fields, "processes")),
		TCPConnections:   int(floatField(row.Fields, "tcp_connections")),
		UptimeSeconds:    int64(floatField(row.Fields, "uptime_seconds")),
	}
}

func floatField(fields map[string]mts.FieldValue, name string) float64 {
	value, ok := fields[name]
	if !ok {
		return 0
	}
	switch value.Type {
	case mts.FieldFloat64:
		return value.Float64
	case mts.FieldInt64:
		return float64(value.Int64)
	default:
		return 0
	}
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
