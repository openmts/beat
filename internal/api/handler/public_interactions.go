package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/deploymentaccess"
	"github.com/beat/backend/internal/model"
)

// 匿名交互被拒时一律返回同一个有界错误，避免泄漏验证细节或计数器。
const publicInteractionRejectionMessage = "public interaction rejected"

// PublicTelemetryPolicyReader 读取匿名聚合遥测策略。
type PublicTelemetryPolicyReader interface {
	Get(ctx context.Context) (model.PublicTelemetryPolicy, error)
}

// PublicInteractionWriter 把已接受的计数写入 MTS。
type PublicInteractionWriter interface {
	WritePublicInteractions(ctx context.Context, at time.Time, interactions []model.PublicInteraction) error
	WritePublicInteractionRejections(ctx context.Context, at time.Time, reasons []string) error
}

// PublicInteractionsHandler 接收一个固定枚举事件；它从不读取身份、地址或自由文本。
type PublicInteractionsHandler struct {
	policies PublicTelemetryPolicyReader
	writer   PublicInteractionWriter
	origins  deploymentaccess.OriginPolicy
	budget   *publicInteractionBudget
	now      func() time.Time
}

func NewPublicInteractionsHandler(
	policies PublicTelemetryPolicyReader,
	writer PublicInteractionWriter,
	origins deploymentaccess.OriginPolicy,
) *PublicInteractionsHandler {
	return &PublicInteractionsHandler{policies: policies, writer: writer, origins: origins,
		budget: newPublicInteractionBudget(publicInteractionBudgetSlots), now: model.NowUTC}
}

// HandlePost 校验来源、内容类型、报文大小、枚举与匿名限流键，然后只累加一个计数。
func (h *PublicInteractionsHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	if h.policies == nil || h.writer == nil {
		JSONError(w, http.StatusServiceUnavailable, "public telemetry unavailable")
		return
	}
	now := h.now()
	policy, err := h.policies.Get(r.Context())
	if err != nil {
		JSONError(w, http.StatusServiceUnavailable, "public telemetry unavailable")
		return
	}
	if !policy.Enabled {
		h.reject(r.Context(), now, model.PublicRejectionDisabled)
		JSONError(w, http.StatusBadRequest, publicInteractionRejectionMessage)
		return
	}
	if !h.originAllowed(r) {
		h.reject(r.Context(), now, model.PublicRejectionUnauthorized)
		JSONError(w, http.StatusBadRequest, publicInteractionRejectionMessage)
		return
	}
	if !h.budget.Allow(clientKey(r), now) {
		h.reject(r.Context(), now, model.PublicRejectionRateLimited)
		JSONError(w, http.StatusBadRequest, publicInteractionRejectionMessage)
		return
	}
	interaction, reason := decodePublicInteraction(w, r)
	if reason != "" {
		h.reject(r.Context(), now, reason)
		JSONError(w, http.StatusBadRequest, publicInteractionRejectionMessage)
		return
	}
	if !policy.Allows(interaction.Event) {
		h.reject(r.Context(), now, model.PublicRejectionEvent)
		JSONError(w, http.StatusBadRequest, publicInteractionRejectionMessage)
		return
	}
	if err := h.writer.WritePublicInteractions(r.Context(), now, []model.PublicInteraction{interaction}); err != nil {
		JSONError(w, http.StatusServiceUnavailable, "public telemetry unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *PublicInteractionsHandler) reject(ctx context.Context, now time.Time, reason string) {
	err := h.writer.WritePublicInteractionRejections(ctx, now, []string{reason})
	_ = err
}

func (h *PublicInteractionsHandler) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if h.origins.Wildcard() {
		return true
	}
	return h.origins.Allows(origin)
}

func decodePublicInteraction(w http.ResponseWriter, r *http.Request) (model.PublicInteraction, string) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return model.PublicInteraction{}, model.PublicRejectionContentType
	}
	body := http.MaxBytesReader(w, r.Body, model.MaximumPublicInteractionBytes)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	var interaction model.PublicInteraction
	if err := decoder.Decode(&interaction); err != nil {
		if errors.Is(err, io.EOF) {
			return model.PublicInteraction{}, model.PublicRejectionBody
		}
		return model.PublicInteraction{}, model.PublicRejectionBody
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return model.PublicInteraction{}, model.PublicRejectionBody
	}
	if !model.ValidPublicInteractionEvent(interaction.Event) {
		return model.PublicInteraction{}, model.PublicRejectionEvent
	}
	if !model.ValidPublicInteractionSurface(interaction.Surface) {
		return model.PublicInteraction{}, model.PublicRejectionSurface
	}
	return interaction, ""
}

// clientKey 只使用有界的来源地址；缺失来源落进同一个桶，绝不生成稳定访客标识。
func clientKey(r *http.Request) string {
	client := strings.TrimSpace(middleware.ClientIP(r))
	if client == "" {
		return "unknown"
	}
	if len(client) > 64 {
		client = client[:64]
	}
	return client
}
