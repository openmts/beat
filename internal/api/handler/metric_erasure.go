package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// MetricErasureHandler 暴露 owner 专属的擦除任务 API；它从不接受 measurement、
// tag、时间范围、SQL 或路径输入。
type MetricErasureHandler struct {
	jobs   *store.MetricErasureStore
	now    func() time.Time
	signal func()
}

func NewMetricErasureHandler(jobs *store.MetricErasureStore) *MetricErasureHandler {
	return &MetricErasureHandler{jobs: jobs, now: model.NowUTC}
}

// WithClock 覆盖当前时间，供测试注入确定时钟。
func (h *MetricErasureHandler) WithClock(now func() time.Time) *MetricErasureHandler {
	h.now = now
	return h
}

// WithSignal 注册创建后的立即唤醒回调，让 202 之后不必等待轮询间隔。
func (h *MetricErasureHandler) WithSignal(signal func()) *MetricErasureHandler {
	h.signal = signal
	return h
}

type metricErasureRequest struct {
	Scope        string `json:"scope"`
	TargetID     string `json:"target_id"`
	RequestID    string `json:"request_id"`
	Confirmation string `json:"confirmation"`
}

type metricErasureView struct {
	ID             string     `json:"id"`
	Scope          string     `json:"scope"`
	State          string     `json:"state"`
	TargetType     string     `json:"target_type"`
	TargetID       string     `json:"target_id"`
	TargetName     string     `json:"target_name"`
	Cutoff         *time.Time `json:"cutoff,omitempty"`
	ProgressCursor string     `json:"progress_cursor,omitempty"`
	Attempts       int        `json:"attempts"`
	ErrorCode      string     `json:"error_code,omitempty"`
	RequestedAt    time.Time  `json:"requested_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type metricErasureListResponse struct {
	Jobs []metricErasureView `json:"jobs"`
}

type metricErasureCreateResponse struct {
	Job      metricErasureView `json:"job"`
	Replayed bool              `json:"replayed"`
}

func (h *MetricErasureHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	limit, ok := metricErasureLimit(r.URL.Query().Get("limit"))
	if !ok {
		JSONError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	jobs, err := h.jobs.List(r.Context(), limit)
	if h.respondError(w, err) {
		return
	}
	views := make([]metricErasureView, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, newMetricErasureView(job))
	}
	JSONResponse(w, http.StatusOK, metricErasureListResponse{Jobs: views})
}

func (h *MetricErasureHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	job, err := h.jobs.Get(r.Context(), r.PathValue("job_id"))
	if h.respondError(w, err) {
		return
	}
	if job == nil {
		JSONError(w, http.StatusNotFound, "metric erasure job not found")
		return
	}
	JSONResponse(w, http.StatusOK, newMetricErasureView(*job))
}

func (h *MetricErasureHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	var body metricErasureRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	job, replayed, err := h.jobs.Create(r.Context(), store.MetricErasureCreate{
		Request: model.MetricErasureCreateRequest{Scope: body.Scope, TargetID: body.TargetID,
			RequestID: body.RequestID, Confirmation: body.Confirmation},
		Now: h.now()})
	if h.respondError(w, err) {
		return
	}
	if !replayed && h.signal != nil {
		h.signal()
	}
	JSONResponse(w, http.StatusAccepted, metricErasureCreateResponse{
		Job: newMetricErasureView(job), Replayed: replayed})
}

func (h *MetricErasureHandler) HandleRetry(w http.ResponseWriter, r *http.Request) {
	h.handleAdminAction(w, r, h.jobs.Retry)
}

func (h *MetricErasureHandler) HandleCancel(w http.ResponseWriter, r *http.Request) {
	h.handleAdminAction(w, r, h.jobs.Cancel)
}

func (h *MetricErasureHandler) handleAdminAction(
	w http.ResponseWriter,
	r *http.Request,
	action func(ctx context.Context, id string, now time.Time) error,
) {
	if h.unavailable(w) {
		return
	}
	id := r.PathValue("job_id")
	if err := action(r.Context(), id, h.now()); h.respondError(w, err) {
		return
	}
	job, err := h.jobs.Get(r.Context(), id)
	if h.respondError(w, err) {
		return
	}
	if job == nil {
		JSONError(w, http.StatusNotFound, "metric erasure job not found")
		return
	}
	if h.signal != nil {
		h.signal()
	}
	JSONResponse(w, http.StatusOK, newMetricErasureView(*job))
}

func (h *MetricErasureHandler) unavailable(w http.ResponseWriter) bool {
	if h != nil && h.jobs != nil {
		return false
	}
	JSONError(w, http.StatusServiceUnavailable, "metric erasure unavailable")
	return true
}

func (h *MetricErasureHandler) respondError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if respondMetadataMutationError(w, err) {
		return true
	}
	switch {
	case errors.Is(err, model.ErrMetricErasureInvalid):
		JSONError(w, http.StatusBadRequest, "invalid metric erasure request")
	case errors.Is(err, model.ErrMetricErasureConflict):
		JSONError(w, http.StatusConflict, "metric erasure conflict")
	case errors.Is(err, model.ErrMetricErasureMissing):
		JSONError(w, http.StatusNotFound, "metric erasure target not found")
	default:
		JSONError(w, http.StatusInternalServerError, "metric erasure failed")
	}
	return true
}

func newMetricErasureView(job model.MetricErasureJob) metricErasureView {
	view := metricErasureView{ID: job.ID, Scope: job.Scope, State: job.State,
		TargetType: job.TargetType, TargetID: job.TargetID, TargetName: job.TargetName,
		ProgressCursor: job.MeasurementCursor, Attempts: job.AttemptCount,
		ErrorCode: job.LastErrorCode, RequestedAt: job.RequestedAt,
		StartedAt: job.StartedAt, CompletedAt: job.CompletedAt}
	if job.CutoffUnixNano != nil {
		cutoff := time.Unix(0, *job.CutoffUnixNano).UTC()
		view.Cutoff = &cutoff
	}
	return view
}

func metricErasureLimit(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, false
	}
	return limit, true
}
