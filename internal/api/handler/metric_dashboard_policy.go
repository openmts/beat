package handler

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func NewDashboardPolicyReader(catalog *store.MetricCatalogStore, nodes *store.NodeStore) DashboardPolicyReader {
	return func(ctx context.Context, public bool) (model.DashboardPolicy, error) {
		policy := model.DashboardPolicy{Public: public, Metrics: make(map[string]model.DashboardMetricPolicy), Nodes: make(map[string]bool)}
		if catalog == nil || nodes == nil {
			return policy, errors.New("metric dashboard policy is unavailable")
		}
		definitions, err := catalog.Catalog(ctx, public)
		if err != nil {
			return policy, err
		}
		for _, metric := range definitions.Metrics {
			policy.Metrics[metric.Key] = model.DashboardMetricPolicy{
				Unit: metric.Unit, Aggregations: metric.Aggregations, Public: metric.Public, Enabled: metric.Enabled,
			}
		}
		visible, err := dashboardPolicyNodes(ctx, nodes, public)
		if err != nil {
			return policy, err
		}
		for _, node := range visible {
			policy.Nodes[node.ID] = true
		}
		return policy, nil
	}
}

func dashboardPolicyNodes(ctx context.Context, nodes *store.NodeStore, public bool) ([]model.Node, error) {
	if public {
		return nodes.ListPublicNodes(ctx, "")
	}
	return nodes.ListNodes(ctx, "")
}
