package handler

import (
	"context"
	"fmt"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type nodeResponse struct {
	model.Node
	Metrics map[string]float64   `json:"metrics"`
	Traffic model.TrafficSummary `json:"traffic"`
}

func buildNodeResponse(
	ctx context.Context,
	node model.Node,
	mtsStore *store.MTSStore,
) (nodeResponse, error) {
	metrics := map[string]float64{}
	totals := model.TrafficTotals{}
	now := model.NowUTC()
	if mtsStore != nil {
		var err error
		metrics, err = mtsStore.QueryLatest(ctx, node.ID)
		if err != nil {
			return nodeResponse{}, fmt.Errorf("query latest metrics: %w", err)
		}
		start, next := model.BillingPeriod(now, node.TrafficResetDay)
		totals, err = mtsStore.QueryTrafficUsage(ctx, node.ID, start, next)
		if err != nil {
			return nodeResponse{}, fmt.Errorf("query traffic usage: %w", err)
		}
	}
	traffic, err := model.SummarizeTraffic(nodeTrafficPolicy(node), totals, now)
	if err != nil {
		return nodeResponse{}, fmt.Errorf("summarize traffic: %w", err)
	}
	return nodeResponse{Node: node, Metrics: metrics, Traffic: traffic}, nil
}

func nodeTrafficPolicy(node model.Node) model.TrafficPolicy {
	return model.TrafficPolicy{
		Limit:     node.TrafficLimit,
		LimitType: node.TrafficLimitType,
		ResetDay:  node.TrafficResetDay,
	}
}
