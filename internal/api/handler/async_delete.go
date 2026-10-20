package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// deletionRequest 是异步删除请求体。确认值必须精确等于实体显示名，服务端在事务内
// 解析名字后比对，因此调用方无法用改名竞态伪造确认。
type deletionRequest struct {
	RequestID    string `json:"request_id"`
	Confirmation string `json:"confirmation"`
}

type deletionAcceptedResponse struct {
	Job      metricErasureView `json:"job"`
	Replayed bool              `json:"replayed"`
}

// createDeletionJob 解析删除请求并创建耐久擦除任务；返回 ok=false 表示已经写出响应。
func createDeletionJob(
	w http.ResponseWriter,
	r *http.Request,
	jobs *store.MetricErasureStore,
	scope string,
	targetID string,
	now time.Time,
) (metricErasureView, bool, bool) {
	if jobs == nil {
		JSONError(w, http.StatusServiceUnavailable, "metric erasure unavailable")
		return metricErasureView{}, false, false
	}
	var body deletionRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return metricErasureView{}, false, false
	}
	job, replayed, err := jobs.Create(r.Context(), store.MetricErasureCreate{
		Request: model.MetricErasureCreateRequest{Scope: scope, TargetID: targetID,
			RequestID: body.RequestID, Confirmation: body.Confirmation}, Now: now})
	if err != nil {
		respondDeletionError(w, err)
		return metricErasureView{}, false, false
	}
	return newMetricErasureView(job), replayed, true
}

func respondDeletionError(w http.ResponseWriter, err error) {
	if respondMetadataMutationError(w, err) {
		return
	}
	if respondOperationLifecycleConflict(w, err) {
		return
	}
	switch {
	case errors.Is(err, model.ErrMetricErasureInvalid):
		JSONError(w, http.StatusBadRequest, "invalid deletion request")
	case errors.Is(err, model.ErrMetricErasureConflict):
		JSONError(w, http.StatusConflict, "deletion already in progress")
	case errors.Is(err, model.ErrMetricErasureMissing):
		JSONError(w, http.StatusNotFound, "deletion target not found")
	default:
		JSONError(w, http.StatusInternalServerError, "failed to request deletion")
	}
}
