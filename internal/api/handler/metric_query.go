package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/metricquery"
)

const maximumMetricRequestBytes = 32 << 10

func (h *MetricCatalogHandler) HandlePublicQuery(w http.ResponseWriter, r *http.Request) {
	h.queryResponse(w, r, true)
}

func (h *MetricCatalogHandler) HandleAdminQuery(w http.ResponseWriter, r *http.Request) {
	h.queryResponse(w, r, false)
}

func (h *MetricCatalogHandler) HandlePublicLatest(w http.ResponseWriter, r *http.Request) {
	h.latestResponse(w, r, true)
}

func (h *MetricCatalogHandler) HandleAdminLatest(w http.ResponseWriter, r *http.Request) {
	h.latestResponse(w, r, false)
}

// latestResponse 与区间查询共用解码、准入与错误映射，即时查询不引入第二套语义。
func (h *MetricCatalogHandler) latestResponse(w http.ResponseWriter, r *http.Request, publicOnly bool) {
	w.Header().Set("Cache-Control", "no-store")
	if h.queries == nil {
		JSONError(w, http.StatusServiceUnavailable, "metric queries are unavailable")
		return
	}
	var request metricquery.LatestRequest
	if !decodeMetricRequest(w, r, &request) {
		return
	}
	result, err := h.queries.Latest(r.Context(), request, metricquery.Access{PublicOnly: publicOnly, ClientKey: middleware.ClientIP(r)})
	if err != nil {
		writeMetricQueryError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, result)
}

func (h *MetricCatalogHandler) queryResponse(w http.ResponseWriter, r *http.Request, publicOnly bool) {
	w.Header().Set("Cache-Control", "no-store")
	if h.queries == nil {
		JSONError(w, http.StatusServiceUnavailable, "metric queries are unavailable")
		return
	}
	var request metricquery.Request
	if !decodeMetricRequest(w, r, &request) {
		return
	}
	result, err := h.queries.Query(r.Context(), request, metricquery.Access{PublicOnly: publicOnly, ClientKey: middleware.ClientIP(r)})
	if err != nil {
		writeMetricQueryError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, result)
}

func decodeMetricRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximumMetricRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return metricRequestDecodeError(w, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return metricRequestDecodeError(w, err)
	}
	return true
}

func metricRequestDecodeError(w http.ResponseWriter, err error) bool {
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		JSONError(w, http.StatusRequestEntityTooLarge, "metric request exceeds 32 KiB")
		return false
	}
	JSONError(w, http.StatusBadRequest, "invalid metric request")
	return false
}

func writeMetricQueryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metricquery.ErrCapacity):
		w.Header().Set("Retry-After", "1")
		JSONError(w, http.StatusTooManyRequests, "metric query capacity exceeded; retry with a shorter range")
	case errors.Is(err, metricquery.ErrTimeout):
		w.Header().Set("Retry-After", "1")
		JSONError(w, http.StatusTooManyRequests, "metric query timed out; retry with a shorter range")
	case errors.Is(err, metricquery.ErrInvalid):
		JSONError(w, http.StatusBadRequest, "invalid or unavailable metric query")
	case errors.Is(err, metricquery.ErrArithmetic):
		JSONError(w, http.StatusUnprocessableEntity, "metric aggregation exceeds the finite numeric range")
	default:
		JSONError(w, http.StatusServiceUnavailable, "metric storage is unavailable")
	}
}
