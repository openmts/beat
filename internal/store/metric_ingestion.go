package store

import (
	"context"
	"errors"
	"math"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

var ErrMetricIngestion = errors.New("custom metric batch contains an unknown, unbound, oversized or non-finite value")

func (catalog *MetricCatalogStore) ValidateCustomBatch(ctx context.Context, nodeID string, values map[string]float64) ([]model.MetricValue, error) {
	return validateCustomBatchFrom(ctx, catalog.db, agentCustomBatch{nodeID: nodeID, values: values})
}

type agentCustomBatch struct {
	nodeID string
	values map[string]float64
}

func validateCustomBatchFrom(ctx context.Context, db schemaExecutor, batch agentCustomBatch) ([]model.MetricValue, error) {
	nodeID, values := batch.nodeID, batch.values
	if err := validateCustomMetricBatch(values); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	bound, err := customMetricIngestionBindings(ctx, db, nodeID)
	if err != nil {
		return nil, err
	}
	accepted := make([]model.MetricValue, 0, len(values))
	for name, value := range values {
		enabled, exists := bound[name]
		if !exists {
			return nil, ErrMetricIngestion
		}
		if enabled {
			accepted = append(accepted, model.MetricValue{Name: model.CustomMetricPrefix + name, Value: value})
		}
	}
	return accepted, nil
}

func validateCustomMetricBatch(values map[string]float64) error {
	if len(values) > maxCustomMetricDefinitionsPerNode {
		return ErrMetricIngestion
	}
	for name, value := range values {
		if !metriccatalog.ValidKey(model.CustomMetricPrefix+name) || math.IsNaN(value) || math.IsInf(value, 0) {
			return ErrMetricIngestion
		}
	}
	return nil
}

func customMetricIngestionBindings(ctx context.Context, db schemaExecutor, nodeID string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT m.name,m.enabled AND d.enabled FROM custom_metrics m
		JOIN custom_metric_nodes n ON n.metric_id=m.id JOIN metric_definitions d ON d.metric_key='custom_'||m.name
		WHERE n.node_id=? LIMIT ?`, nodeID, maxCustomMetricTargets+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	bound := make(map[string]bool)
	for rows.Next() {
		var name string
		var enabled bool
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, err
		}
		bound[name] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(bound) > maxCustomMetricTargets {
		return nil, ErrMetricIngestion
	}
	return bound, nil
}
