package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type NodeService struct {
	nodeStore *store.NodeStore
	mtsStore  *store.MTSStore
}

func NewNodeService(nodeStore *store.NodeStore, mtsStore *store.MTSStore) *NodeService {
	return &NodeService{
		nodeStore: nodeStore,
		mtsStore:  mtsStore,
	}
}

func (s *NodeService) RegisterNode(ctx context.Context, host string, port int, metric *model.NodeMetrics) (*model.Node, error) {
	node, err := s.nodeStore.UpsertNode(ctx, host, host, port)
	if err != nil {
		return nil, fmt.Errorf("upsert node: %w", err)
	}

	if metric != nil {
		now := model.NowUTC()
		if err := s.writeMetrics(ctx, node.ID, metric, now); err != nil {
			return nil, err
		}
	}

	return node, nil
}

func (s *NodeService) writeMetrics(
	ctx context.Context,
	nodeID string,
	metric *model.NodeMetrics,
	now time.Time,
) error {
	if err := s.mtsStore.WriteNodeMetrics(ctx, store.NodeMetricSample{
		NodeID: nodeID, Metrics: *metric, Timestamp: now,
	}); err != nil {
		return fmt.Errorf("write node metrics: %w", err)
	}
	return nil
}

func (s *NodeService) GetNodeMetrics(ctx context.Context, nodeID string, metrics []string, start, end time.Time) (map[string][]model.MetricData, error) {
	tsPoints, err := s.mtsStore.QueryMetrics(ctx, metrics, start, end, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query metrics: %w", err)
	}

	result := map[string][]model.MetricData{}
	for _, m := range metrics {
		points, ok := tsPoints[m]
		if !ok {
			result[m] = []model.MetricData{}
			continue
		}
		data := make([]model.MetricData, 0, len(points))
		for _, p := range points {
			data = append(data, model.MetricData{
				Timestamp: p.Timestamp,
				Value:     p.Value,
			})
		}
		result[m] = data
	}
	return result, nil
}

type NodeSummary struct {
	CPU         float64 `json:"cpu"`
	CPUUsed     float64 `json:"cpu_used"`
	CPUTotal    float64 `json:"cpu_total"`
	Memory      float64 `json:"memory"`
	MemoryUsed  float64 `json:"memory_used"`
	MemoryTotal float64 `json:"memory_total"`
	DiskUsed    float64 `json:"disk_used"`
	DiskTotal   float64 `json:"disk_total"`
	DiskRead    float64 `json:"disk_read"`
	DiskWrite   float64 `json:"disk_write"`
	NetRecv     float64 `json:"net_recv"`
	NetSent     float64 `json:"net_sent"`
	UpdatedAt   string  `json:"updated_at"`
}

func (s *NodeService) GetNodeSummary(ctx context.Context, nodeID string) (*NodeSummary, error) {
	metrics, err := s.mtsStore.QueryLatest(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query latest metrics: %w", err)
	}

	if len(metrics) == 0 {
		return &NodeSummary{}, nil
	}

	return &NodeSummary{
		CPU:         metrics["cpu"],
		CPUUsed:     metrics["cpu_used"],
		CPUTotal:    metrics["cpu_total"],
		Memory:      metrics["memory"],
		MemoryUsed:  metrics["memory_used"],
		MemoryTotal: metrics["memory_total"],
		DiskUsed:    metrics["disk_used"],
		DiskTotal:   metrics["disk_total"],
		DiskRead:    metrics["disk_read"],
		DiskWrite:   metrics["disk_write"],
		NetRecv:     metrics["net_recv"],
		NetSent:     metrics["net_sent"],
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

var _ = uuid.NewString
