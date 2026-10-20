package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

var ErrPublicMetricQuery = errors.New("public metric storage query failed")

type nodeResponse struct {
	Accelerators       []model.AcceleratorDevicePublic `json:"accelerators,omitempty"`
	AcceleratorVisible bool                            `json:"accelerator_visible"`
	Location           *model.LocationPublic           `json:"location,omitempty"`
	Lifecycle          *model.LifecyclePublic          `json:"lifecycle,omitempty"`
	model.Node
	model.TelemetryFreshness
	Metrics map[string]float64   `json:"metrics"`
	Traffic model.TrafficSummary `json:"traffic"`
}

type publicNodeResponse struct {
	nodeResponse
	Traffic *model.TrafficSummary `json:"traffic,omitempty"`
}

type nodeViewPolicy struct {
	node    model.Node
	metrics map[string]bool
}

func buildNodeResponse(
	ctx context.Context,
	node model.Node,
	mtsStore *store.MTSStore,
	resolver *store.TrafficAccountingResolver,
) (nodeResponse, error) {
	return buildNodeResponseWithPolicy(ctx, mtsStore, resolver, nodeViewPolicy{node: node})
}

func buildNodeResponseWithPolicy(
	ctx context.Context,
	mtsStore *store.MTSStore,
	resolver *store.TrafficAccountingResolver,
	view nodeViewPolicy,
) (nodeResponse, error) {
	node := view.node
	metrics := map[string]float64{}
	totals := model.TrafficTotals{}
	accounting := model.TrafficAccounting{Mode: model.TrafficAccountingRaw}
	now := model.NowUTC()
	freshness := model.NewTelemetryFreshness(now, nil, 0)
	if mtsStore != nil {
		var err error
		snapshot, snapshotErr := mtsStore.QueryAgentSnapshot(ctx, node.ID, view.metrics)
		metrics, err = snapshot.Metrics, snapshotErr
		if err != nil {
			return nodeResponse{}, fmt.Errorf("%w: query latest metrics: %w", ErrPublicMetricQuery, err)
		}
		if receipt := snapshot.Receipt; receipt != nil {
			freshness = model.NewTelemetryFreshness(now, &receipt.CapturedAt, time.Duration(receipt.ReportIntervalMS)*time.Millisecond)
		}
		if trafficMetricsVisible(view.metrics) {
			totals, accounting, err = resolveCycleTraffic(ctx, mtsStore, resolver, node, now)
			if err != nil {
				return nodeResponse{}, fmt.Errorf("%w: query traffic usage: %w", ErrPublicMetricQuery, err)
			}
		}
	}
	traffic, err := model.SummarizeTraffic(nodeTrafficPolicy(node), totals, now)
	if err != nil {
		return nodeResponse{}, fmt.Errorf("summarize traffic: %w", err)
	}
	traffic.AccountingMode = accounting.Mode
	traffic.CalibratedAt = accounting.CalibratedAt
	return nodeResponse{Node: node, Metrics: metrics, Traffic: traffic, TelemetryFreshness: freshness}, nil
}

// resolveCycleTraffic 优先走校准解析器；没有注入时退回原始 MTS 聚合。
func resolveCycleTraffic(
	ctx context.Context,
	mtsStore *store.MTSStore,
	resolver *store.TrafficAccountingResolver,
	node model.Node,
	now time.Time,
) (model.TrafficTotals, model.TrafficAccounting, error) {
	if resolver != nil {
		return resolver.CycleTotals(ctx, node, now)
	}
	start, next := model.BillingPeriod(now, node.TrafficResetDay)
	totals, err := mtsStore.QueryTrafficUsage(ctx, node.ID, start, next)
	if err != nil {
		return model.TrafficTotals{}, model.TrafficAccounting{}, err
	}
	return totals, model.TrafficAccounting{Mode: model.TrafficAccountingRaw}, nil
}

func trafficMetricsVisible(allowed map[string]bool) bool {
	return allowed == nil || (allowed["net_recv_delta"] && allowed["net_sent_delta"])
}

func (h *NodeHandler) buildPublicNodeResponse(ctx context.Context, node model.Node, settings model.SiteSettings) (publicNodeResponse, error) {
	var allowed map[string]bool
	if h.metricCatalog != nil {
		var err error
		allowed, err = h.metricCatalog.PublicKeys(ctx)
		if err != nil {
			return publicNodeResponse{}, err
		}
	}
	base, err := buildNodeResponseWithPolicy(ctx, h.mtsStore, h.trafficResolver, nodeViewPolicy{node: node, metrics: allowed})
	if err != nil {
		return publicNodeResponse{}, err
	}
	result := publicNodeResponse{nodeResponse: base}
	if h.locationViews != nil {
		location, err := h.locationViews.PublicLocation(ctx, node.ID)
		if err != nil {
			return publicNodeResponse{}, err
		}
		result.Location = location
	}
	if h.lifecycleViews != nil {
		lifecycle, err := h.lifecycleViews.PublicLifecycle(ctx, node.ID)
		if err != nil {
			return publicNodeResponse{}, err
		}
		result.Lifecycle = lifecycle
	}
	if err := h.addPublicAccelerators(ctx, node.ID, settings.ShowAccelerators, &result); err != nil {
		return publicNodeResponse{}, err
	}
	if trafficMetricsVisible(allowed) {
		result.Traffic = &base.Traffic
	}
	return result, nil
}

func nodeTrafficPolicy(node model.Node) model.TrafficPolicy {
	return model.TrafficPolicy{
		Limit:     node.TrafficLimit,
		LimitType: node.TrafficLimitType,
		ResetDay:  node.TrafficResetDay,
	}
}

func (h *NodeHandler) addPublicAccelerators(
	ctx context.Context,
	nodeID string,
	globalVisible bool,
	result *publicNodeResponse,
) error {
	if h.accelerators == nil {
		return nil
	}
	nodeVisible, err := h.accelerators.PublicVisible(ctx, nodeID)
	if err != nil {
		return err
	}
	if !globalVisible || !nodeVisible {
		return nil
	}
	devices, err := h.accelerators.PublicAccelerators(ctx, nodeID)
	if err != nil {
		return err
	}
	result.AcceleratorVisible = true
	result.Accelerators = devices
	return nil
}
