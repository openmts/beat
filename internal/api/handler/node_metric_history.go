package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type legacyMetricRequest struct {
	nodeID   string
	metrics  []string
	from, to time.Time
}

func (h *NodeHandler) HandleGetNodeMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	node, err := h.nodeStore.GetPublicNode(r.Context(), nodeID)
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to get node")
		return
	}
	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")
		return
	}
	request, err := h.parseLegacyMetricRequest(r)
	if err != nil {
		writeMetricQueryError(w, err)
		return
	}
	result, err := h.readLegacyMetrics(r, request)
	if err != nil {
		if errors.Is(err, metricquery.ErrInvalid) || errors.Is(err, metricquery.ErrCapacity) {
			writeMetricQueryError(w, err)
		} else {
			JSONError(w, http.StatusInternalServerError, "failed to read metrics")
		}
		return
	}
	JSONResponse(w, http.StatusOK, legacyMetricResponse(nodeID, result))
}

func (h *NodeHandler) parseLegacyMetricRequest(r *http.Request) (legacyMetricRequest, error) {
	request := legacyMetricRequest{nodeID: r.PathValue("id"), metrics: defaultMetrics}
	var err error
	request.from, err = time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		return request, metricquery.ErrInvalid
	}
	request.to, err = time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err != nil {
		return request, metricquery.ErrInvalid
	}
	parameter := r.URL.Query().Get("metric")
	if parameter != "" {
		request.metrics = strings.Split(parameter, ",")
	}
	if h.metricCatalog != nil {
		allowed, err := h.metricCatalog.PublicKeys(r.Context())
		if err != nil {
			return request, metricquery.ErrUnavailable
		}
		request.metrics, err = allowedLegacyMetrics(request.metrics, allowed, parameter != "")
		if err != nil {
			return request, err
		}
	}
	if err := validateLegacyMetricBudget(request, parameter != "", model.NowUTC()); err != nil {
		return request, err
	}
	return request, nil
}

// validateLegacyMetricBudget 在触碰 MTS 之前拒绝超出预算的公共历史请求：显式选择只允许
// 1 到 8 个不重复指标，区间必须为正、不超过 31 天且不是纯未来区间。默认（未显式选择）
// 路径只返回目录中的公开指标，但仍受同一区间约束。
func validateLegacyMetricBudget(request legacyMetricRequest, explicit bool, now time.Time) error {
	if explicit {
		if len(request.metrics) == 0 || len(request.metrics) > metricquery.MaximumSeries {
			return metricquery.ErrInvalid
		}
		seen := make(map[string]bool, len(request.metrics))
		for _, metric := range request.metrics {
			if seen[metric] {
				return metricquery.ErrInvalid
			}
			seen[metric] = true
		}
	} else if len(request.metrics) == 0 {
		return metricquery.ErrInvalid
	}
	if !request.to.After(request.from) || request.to.Sub(request.from) > metricquery.MaximumRange {
		return metricquery.ErrInvalid
	}
	if request.from.After(now) {
		return metricquery.ErrInvalid
	}
	return nil
}

func allowedLegacyMetrics(metrics []string, allowed map[string]bool, explicit bool) ([]string, error) {
	filtered := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		if allowed[metric] {
			filtered = append(filtered, metric)
		} else if explicit {
			return nil, metricquery.ErrInvalid
		}
	}
	return filtered, nil
}

func (h *NodeHandler) readLegacyMetrics(r *http.Request, request legacyMetricRequest) (map[string][]store.TimePoint, error) {
	if h.mtsStore == nil {
		return nil, metricquery.ErrUnavailable
	}
	interval, buckets, ok := metricquery.PlanBuckets(request.from, request.to, metricquery.MaximumPoints)
	if !ok {
		return nil, metricquery.ErrInvalid
	}
	aggregations, err := h.legacyAggregations(r.Context(), request.metrics)
	if err != nil {
		return nil, err
	}
	key := historyShareKey(request.nodeID, request.from.UTC().Format(time.RFC3339Nano),
		request.to.UTC().Format(time.RFC3339Nano), strings.Join(request.metrics, ","),
		interval.String(), strconv.Itoa(buckets))
	var result map[string][]store.TimePoint
	read := func(ctx context.Context) error {
		merged, err := h.historyShare.do(ctx, key, func(ctx context.Context) (map[string][]store.TimePoint, error) {
			return h.readLegacyMetricChunks(ctx, request, aggregations, interval, buckets)
		})
		if err != nil {
			return err
		}
		result = merged
		return nil
	}
	if h.queryBudget != nil {
		err = h.queryBudget.ReadWithBudget(r.Context(), middleware.ClientIP(r), read)
	} else {
		err = read(r.Context())
	}
	return result, err
}

// readLegacyMetricChunks 把默认（未显式选择）的公开指标集按每次最多 8 个指标分块读取，
// 保持“单次查询不超过指标预算”的契约，同时不改变默认路径返回全部公开指标的行为。
func (h *NodeHandler) readLegacyMetricChunks(
	ctx context.Context,
	request legacyMetricRequest,
	aggregations map[string]string,
	interval time.Duration,
	buckets int,
) (map[string][]store.TimePoint, error) {
	merged := make(map[string][]store.TimePoint, len(request.metrics))
	for start := 0; start < len(request.metrics); start += metricquery.MaximumSeries {
		chunk := request.metrics[start:min(start+metricquery.MaximumSeries, len(request.metrics))]
		chunkAggregations := make(map[string]string, len(chunk))
		for _, metric := range chunk {
			chunkAggregations[metric] = aggregations[metric]
		}
		points, err := h.mtsStore.QueryBucketedMetrics(ctx, store.BucketedMetricRange{
			Metrics: chunk, Aggregations: chunkAggregations, NodeID: request.nodeID,
			From: request.from, To: request.to, Interval: interval, Buckets: buckets})
		if err != nil {
			return nil, err
		}
		for metric, series := range points {
			merged[metric] = series
		}
	}
	return merged, nil
}

// legacyAggregations 为每个指标选择确定性的聚合函数：目录可用时使用该指标的默认聚合
// （尊重运营方的策略更新），没有目录时退回内置定义的默认值。
func (h *NodeHandler) legacyAggregations(ctx context.Context, metrics []string) (map[string]string, error) {
	aggregations := make(map[string]string, len(metrics))
	for _, metric := range metrics {
		aggregation := metriccatalog.DefaultAggregationForKey(metric)
		if h.metricCatalog != nil {
			definition, err := h.metricCatalog.Get(ctx, metric)
			if err != nil {
				return nil, metricquery.ErrUnavailable
			}
			if definition == nil {
				return nil, metricquery.ErrInvalid
			}
			aggregation = definition.DefaultAggregation
		}
		aggregations[metric] = aggregation
	}
	return aggregations, nil
}

func legacyMetricResponse(nodeID string, result map[string][]store.TimePoint) map[string][]metricResponse {
	output := make(map[string][]metricResponse, len(result))
	for metric, points := range result {
		values := make([]metricResponse, 0, len(points))
		for _, point := range points {
			values = append(values, metricResponse{NodeID: nodeID, Metric: metric, Value: point.Value, Timestamp: point.Timestamp.Unix()})
		}
		output[metric] = values
	}
	return output
}
