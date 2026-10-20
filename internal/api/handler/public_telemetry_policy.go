package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// PublicTelemetryPolicyStore 是 owner 读写匿名聚合遥测策略的唯一入口。
type PublicTelemetryPolicyStore interface {
	Get(ctx context.Context) (model.PublicTelemetryPolicy, error)
	Update(ctx context.Context, policy model.PublicTelemetryPolicy) (model.PublicTelemetryPolicy, error)
}

// PublicTelemetryHandler 只暴露策略本身；它从不下发来源列表、样本或访客信息。
type PublicTelemetryHandler struct {
	store PublicTelemetryPolicyStore
}

func NewPublicTelemetryHandler(store PublicTelemetryPolicyStore) *PublicTelemetryHandler {
	return &PublicTelemetryHandler{store: store}
}

func (handler *PublicTelemetryHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if handler.store == nil {
		JSONError(w, http.StatusServiceUnavailable, "public telemetry unavailable")
		return
	}
	policy, err := handler.store.Get(r.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to load public telemetry policy")
		return
	}
	JSONResponse(w, http.StatusOK, policy)
}

func (handler *PublicTelemetryHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	if handler.store == nil {
		JSONError(w, http.StatusServiceUnavailable, "public telemetry unavailable")
		return
	}
	var policy model.PublicTelemetryPolicy
	if err := ParseJSONStrict(r, &policy); err != nil {
		WriteBodyError(w, err)
		return
	}
	policy.Normalize()
	if err := policy.Validate(); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := handler.store.Update(r.Context(), policy)
	if respondMetadataMutationError(w, err) {
		return
	}
	if errors.Is(err, store.ErrPublicTelemetryConflict) {
		JSONError(w, http.StatusConflict, "public telemetry policy conflict")
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to update public telemetry policy")
		return
	}
	JSONResponse(w, http.StatusOK, updated)
}
