package tsdb

import (
	"context"
	"time"

	"beat/internal/domain"
)

type Stats struct {
	Driver       string        `json:"driver"`
	Path         string        `json:"path"`
	Healthy      bool          `json:"healthy"`
	Ready        bool          `json:"ready"`
	Measurements int           `json:"measurements"`
	SizeBytes    int64         `json:"sizeBytes"`
	Reasons      []string      `json:"reasons,omitempty"`
	Checks       []HealthCheck `json:"checks,omitempty"`
}

type HealthCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type Backend interface {
	Append(ctx context.Context, nodeID string, metrics domain.Metrics) error
	AppendBatch(ctx context.Context, nodeID string, points []domain.Metrics) error
	Query(ctx context.Context, nodeID string, from, to time.Time) ([]domain.Metrics, error)
	QueryAll(ctx context.Context, from, to time.Time) (map[string][]domain.Metrics, error)
	AppendNetworkProbes(ctx context.Context, nodeID string, points []domain.NetworkProbePoint) error
	QueryNetworkProbes(ctx context.Context, nodeID string, from, to time.Time) ([]domain.NetworkProbePoint, error)
	AppendTaskMetrics(ctx context.Context, nodeID, measurement, taskID, taskName string, timestamp time.Time, metrics map[string]float64) error
	DeleteNode(ctx context.Context, nodeID string) error
	Stats(ctx context.Context) (Stats, error)
	Close(ctx context.Context) error
}
