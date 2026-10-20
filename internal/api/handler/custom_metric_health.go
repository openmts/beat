package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const (
	// 采样年龄超过「三倍采集间隔」或「一分钟」中的较大者即视为陈旧，
	// 允许单次采集抖动与一次失败重试而不误报。
	customMetricHealthStaleIntervals = 3
	customMetricHealthMinimumStale   = time.Minute
)

const (
	customMetricHealthStateOK         = "ok"
	customMetricHealthStateStale      = "stale"
	customMetricHealthStateMissing    = "missing"
	customMetricHealthStateUnverified = "unverified"
	customMetricHealthStateDisabled   = "disabled"
)

// customSampleSource 读取最近一次自定义指标采样及其来源证据。
type customSampleSource interface {
	QueryCustomSample(ctx context.Context, nodeID, metric string) (*store.CustomMetricEvidence, error)
}

// WithCustomSources 注入采样证据读取能力；未注入时健康端点返回 503。
func (h *CustomMetricHandler) WithCustomSources(samples customSampleSource) *CustomMetricHandler {
	h.samples = samples
	return h
}

type customMetricHealthMetric struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	IntervalSeconds   int    `json:"interval_seconds"`
	StaleAfterSeconds int64  `json:"stale_after_seconds"`
}

// customMetricHealthNode 描述单个绑定节点的采集健康度；缺失字段表示尚无采样。
type customMetricHealthNode struct {
	NodeID            string     `json:"node_id"`
	State             string     `json:"state"`
	Value             *float64   `json:"value,omitempty"`
	CapturedAt        *time.Time `json:"captured_at,omitempty"`
	ReportAt          *time.Time `json:"report_at,omitempty"`
	AgeSeconds        int64      `json:"age_seconds"`
	SampleID          string     `json:"sample_id,omitempty"`
	CadenceMS         int64      `json:"cadence_ms,omitempty"`
	DefinitionVersion string     `json:"definition_version,omitempty"`
}

type customMetricHealthResponse struct {
	Metric customMetricHealthMetric `json:"metric"`
	Nodes  []customMetricHealthNode `json:"nodes"`
}

// HandleHealth 汇总某个自定义指标在各绑定节点上的最近采样与陈旧情况。
func (h *CustomMetricHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if h.samples == nil {
		JSONError(w, http.StatusServiceUnavailable, "custom metric health is unavailable")
		return
	}
	id := r.PathValue("id")
	metric, err := h.store.Get(r.Context(), id)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get custom metric")
		return
	}
	if metric == nil {
		JSONError(w, http.StatusNotFound, "custom metric not found")
		return
	}
	nodeIDs, err := h.store.NodeIDsForMetric(r.Context(), id)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to list custom metric nodes")
		return
	}
	now := time.Now().UTC()
	nodes := make([]customMetricHealthNode, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := h.customMetricHealth(r.Context(), *metric, nodeID, now)
		if err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to read custom metric health")
			return
		}
		nodes = append(nodes, node)
	}
	JSONResponse(w, http.StatusOK, customMetricHealthResponse{
		Metric: customMetricHealthMetric{
			ID:                metric.ID,
			Name:              metric.Name,
			Enabled:           metric.Enabled,
			IntervalSeconds:   metric.IntervalSeconds,
			StaleAfterSeconds: int64(customMetricStaleAfter(metric.IntervalSeconds).Seconds()),
		},
		Nodes: nodes,
	})
}

func (h *CustomMetricHandler) customMetricHealth(
	ctx context.Context,
	metric model.CustomMetric,
	nodeID string,
	now time.Time,
) (customMetricHealthNode, error) {
	node := customMetricHealthNode{NodeID: nodeID}
	if !metric.Enabled {
		node.State = customMetricHealthStateDisabled
		return node, nil
	}
	evidence, err := h.samples.QueryCustomSample(ctx, nodeID, model.CustomMetricPrefix+metric.Name)
	if err != nil && !errors.Is(err, store.ErrCustomSampleSourceUnavailable) {
		return node, fmt.Errorf("query custom sample: %w", err)
	}
	if err != nil || evidence == nil {
		// 采样存在但缺少来源回执时导出 unverified，避免把纯 MTS 数值当成可信采样。
		node.State = customMetricHealthStateMissing
		if errors.Is(err, store.ErrCustomSampleSourceUnavailable) {
			node.State = customMetricHealthStateUnverified
		}
		return node, nil
	}
	value := evidence.Value
	capturedAt := evidence.Source.CapturedAt
	reportAt := evidence.ReportAt
	node.Value, node.CapturedAt, node.ReportAt = &value, &capturedAt, &reportAt
	node.SampleID = evidence.Source.SampleID
	node.CadenceMS = evidence.Source.CadenceMS
	node.DefinitionVersion = evidence.Source.DefinitionVersion
	node.AgeSeconds = int64(now.Sub(evidence.ReportAt).Seconds())
	staleAfter := customMetricStaleAfter(metric.IntervalSeconds)
	if now.Sub(evidence.ReportAt) > staleAfter {
		node.State = customMetricHealthStateStale
		return node, nil
	}
	node.State = customMetricHealthStateOK
	return node, nil
}

func customMetricStaleAfter(intervalSeconds int) time.Duration {
	stale := time.Duration(intervalSeconds) * time.Second * customMetricHealthStaleIntervals
	return max(stale, customMetricHealthMinimumStale)
}
