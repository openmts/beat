package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

// 仅供隔离主题浏览器场景使用，响应统一指标协议和显式缺口。
func themeMetricFixtureAPI(w http.ResponseWriter, request *http.Request) bool {
	switch request.URL.Path {
	case "/api/v1/metrics/catalog":
		JSONResponse(w, http.StatusOK, map[string]any{"version": 1, "revision": 1, "metrics": metriccatalog.Builtins()})
	case "/api/v1/metrics/dashboards/default":
		dashboard := model.DefaultMetricDashboard("public")
		dashboard.Definition.RefreshSeconds = 5
		JSONResponse(w, http.StatusOK, dashboard)
	case "/api/v1/metrics/query":
		themeMetricFixtureQuery(w, request)
	default:
		return false
	}
	return true
}

type themeMetricQuery struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Series []struct {
		RefID       string `json:"ref_id"`
		NodeID      string `json:"node_id"`
		Metric      string `json:"metric"`
		Aggregation string `json:"aggregation"`
	} `json:"series"`
}

func themeMetricFixtureQuery(w http.ResponseWriter, request *http.Request) {
	var query themeMetricQuery
	if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid fixture query")
		return
	}
	from, fromErr := time.Parse(time.RFC3339Nano, query.From)
	to, toErr := time.Parse(time.RFC3339Nano, query.To)
	if fromErr != nil || toErr != nil || !from.Before(to) {
		JSONError(w, http.StatusBadRequest, "invalid fixture range")
		return
	}
	definitions := map[string]model.MetricDefinition{}
	for _, definition := range metriccatalog.Builtins() {
		definitions[definition.Key] = definition
	}
	series := make([]map[string]any, 0, len(query.Series))
	for _, requested := range query.Series {
		definition := definitions[requested.Metric]
		series = append(series, map[string]any{"ref_id": requested.RefID, "node_id": requested.NodeID, "metric": requested.Metric,
			"aggregation": requested.Aggregation, "label": definition.Label, "unit": definition.Unit, "available_from": query.From, "state": "ok",
			"points": themeFixturePoints(from.UnixMilli(), to.UnixMilli())})
	}
	JSONResponse(w, http.StatusOK, map[string]any{"from": query.From, "to": query.To, "interval_ms": (to.UnixMilli() - from.UnixMilli()) / 12, "series": series})
}

func themeFixturePoints(from, to int64) []map[string]any {
	points := make([]map[string]any, 0, 13)
	for index := 0; index <= 12; index++ {
		var value any = 35 + math.Sin(float64(index)/2)*12
		if index == 6 {
			value = nil
		}
		timestamp := from + (to-from)*int64(index)/12
		if timestamp >= to {
			timestamp = to - 1
		}
		points = append(points, map[string]any{"timestamp": timestamp, "value": value})
	}
	return points
}
