package store

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func validateMetricDependencies(ctx context.Context, db schemaExecutor, next model.MetricDefinition) error {
	key, enabled := metricDependency(next)
	if key == "" {
		return nil
	}
	definition, err := findMetricDefinition(ctx, db, key)
	if err != nil {
		return err
	}
	if definition == nil || definition.Enabled != enabled {
		return fmt.Errorf("%w: %s requires %s enabled=%t", metriccatalog.ErrInvalidDefinition, next.Key, key, enabled)
	}
	return nil
}

func metricDependency(next model.MetricDefinition) (string, bool) {
	switch next.Key {
	case "net_recv_total":
		if !next.Enabled {
			return "net_recv_delta", false
		}
	case "net_sent_total":
		if !next.Enabled {
			return "net_sent_delta", false
		}
	case "net_recv_delta":
		if next.Enabled {
			return "net_recv_total", true
		}
	case "net_sent_delta":
		if next.Enabled {
			return "net_sent_total", true
		}
	}
	return "", false
}
