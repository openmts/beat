package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// TrafficCalibrationHandler 暴露 owner 专属的追加式流量校准 API。
type TrafficCalibrationHandler struct {
	nodes  *store.NodeStore
	mts    *store.MTSStore
	ledger *store.TrafficCalibrationStore
	now    func() time.Time
}

func NewTrafficCalibrationHandler(
	nodes *store.NodeStore,
	mts *store.MTSStore,
	ledger *store.TrafficCalibrationStore,
) *TrafficCalibrationHandler {
	return &TrafficCalibrationHandler{nodes: nodes, mts: mts, ledger: ledger, now: model.NowUTC}
}

// WithClock 覆盖当前时间，供测试注入确定时钟。
func (h *TrafficCalibrationHandler) WithClock(now func() time.Time) *TrafficCalibrationHandler {
	h.now = now
	return h
}

type trafficCalibrationRequest struct {
	RequestID      string     `json:"request_id"`
	EffectiveAt    *time.Time `json:"effective_at"`
	TargetSent     string     `json:"target_sent"`
	TargetReceived string     `json:"target_received"`
	Reason         string     `json:"reason"`
}

type trafficCalibrationRevokeRequest struct {
	RequestID string `json:"request_id"`
}

type trafficCalibrationPayload struct {
	request  trafficCalibrationRequest
	sent     int64
	received int64
}

type trafficCalibrationResponse struct {
	Event    trafficCalibrationView  `json:"event"`
	Revoked  *trafficCalibrationView `json:"revoked,omitempty"`
	Replayed bool                    `json:"replayed"`
}

type trafficCalibrationHistoryResponse struct {
	Events     []trafficCalibrationView `json:"events"`
	NextCursor string                   `json:"next_cursor,omitempty"`
}

func (h *TrafficCalibrationHandler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	node, ok := h.loadNode(w, r)
	if !ok {
		return
	}
	limit, err := calibrationHistoryLimit(r.URL.Query().Get("limit"))
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	page, err := h.ledger.History(r.Context(), store.TrafficCalibrationHistoryRequest{
		NodeID: node.ID, Limit: limit, Cursor: r.URL.Query().Get("cursor")})
	if h.respondError(w, err) {
		return
	}
	JSONResponse(w, http.StatusOK, trafficCalibrationHistoryResponse{
		Events: newTrafficCalibrationViews(page.Events), NextCursor: page.NextCursor})
}

func (h *TrafficCalibrationHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	node, ok := h.loadNode(w, r)
	if !ok {
		return
	}
	payload, ok := decodeCalibrationPayload(w, r)
	if !ok {
		h.recordRejection(r, node, calibrationRejectInvalid)
		return
	}
	now := h.now()
	effective, err := h.selectEffective(r.Context(), node, payload.request.EffectiveAt, now)
	if h.respondRejected(w, r, node, err) {
		return
	}
	change, err := h.ledger.AppendSet(r.Context(), store.TrafficCalibrationSetWrite{
		Request: calibrationModelRequest(payload),
		NodeID:  node.ID, NodeName: node.Name, EffectiveAt: effective,
		Actor: calibrationActor(r, payload.request.RequestID), Now: now})
	if h.respondRejected(w, r, node, err) {
		return
	}
	JSONResponse(w, http.StatusCreated, calibrationChangeResponse(change))
}

func (h *TrafficCalibrationHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	node, ok := h.loadNode(w, r)
	if !ok {
		return
	}
	payload, ok := decodeRevokePayload(w, r)
	if !ok {
		h.recordRejection(r, node, calibrationRejectInvalid)
		return
	}
	now := h.now()
	start, next := model.BillingPeriod(now, node.TrafficResetDay)
	change, err := h.ledger.AppendRevoke(r.Context(), store.TrafficCalibrationRevokeWrite{
		RequestID: payload.RequestID, NodeName: node.Name,
		Window:  store.TrafficCalibrationWindow{NodeID: node.ID, Start: start, End: next},
		EventID: r.PathValue("calibration_id"),
		Actor:   calibrationActor(r, payload.RequestID), Now: now})
	if h.respondRejected(w, r, node, err) {
		return
	}
	JSONResponse(w, http.StatusOK, calibrationChangeResponse(change))
}

func (h *TrafficCalibrationHandler) HandleReplace(w http.ResponseWriter, r *http.Request) {
	node, ok := h.loadNode(w, r)
	if !ok {
		return
	}
	payload, ok := decodeCalibrationPayload(w, r)
	if !ok {
		h.recordRejection(r, node, calibrationRejectInvalid)
		return
	}
	now := h.now()
	effective, err := h.selectEffective(r.Context(), node, payload.request.EffectiveAt, now)
	if h.respondRejected(w, r, node, err) {
		return
	}
	start, next := model.BillingPeriod(now, node.TrafficResetDay)
	change, err := h.ledger.AppendReplace(r.Context(), store.TrafficCalibrationReplaceWrite{
		Request:  calibrationModelRequest(payload),
		NodeName: node.Name, EffectiveAt: effective,
		Window:  store.TrafficCalibrationWindow{NodeID: node.ID, Start: start, End: next},
		EventID: r.PathValue("calibration_id"),
		Actor:   calibrationActor(r, payload.request.RequestID), Now: now})
	if h.respondRejected(w, r, node, err) {
		return
	}
	JSONResponse(w, http.StatusOK, calibrationChangeResponse(change))
}

func (h *TrafficCalibrationHandler) loadNode(w http.ResponseWriter, r *http.Request) (model.Node, bool) {
	if h == nil || h.nodes == nil || h.ledger == nil {
		JSONError(w, http.StatusServiceUnavailable, "traffic calibration unavailable")
		return model.Node{}, false
	}
	node, err := h.nodes.GetNode(r.Context(), r.PathValue("id"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "failed to read node")
		return model.Node{}, false
	}
	if node == nil {
		JSONError(w, http.StatusNotFound, "node not found")
		return model.Node{}, false
	}
	return *node, true
}

// selectEffective 在持久的流量写入边界内挑出校准生效秒。
//
// 生效秒永远不晚于边界，因此任何后续样本（Agent 报告强制严格递增）都落在边界之后，
// 既不会被“目标 + 边界之后增量”的结算漏掉，也不会与目标重复计数。
func (h *TrafficCalibrationHandler) selectEffective(
	ctx context.Context,
	node model.Node,
	requested *time.Time,
	now time.Time,
) (time.Time, error) {
	start, _ := model.BillingPeriod(now, node.TrafficResetDay)
	safe := now.UTC().Truncate(time.Second)
	if h.mts != nil {
		boundary, found, err := h.mts.TrafficAccountingBoundary(ctx, node.ID)
		if err != nil {
			return time.Time{}, err
		}
		if found && boundary.Before(safe) {
			safe = boundary
		}
	}
	if safe.Before(start) {
		safe = start
	}
	if requested == nil {
		return safe, nil
	}
	want := requested.UTC()
	if !want.Equal(want.Truncate(time.Second)) || want.Before(start) || want.After(safe) {
		return time.Time{}, model.ErrTrafficCalibrationInvalid
	}
	return want, nil
}

func decodeCalibrationPayload(w http.ResponseWriter, r *http.Request) (trafficCalibrationPayload, bool) {
	var body trafficCalibrationRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return trafficCalibrationPayload{}, false
	}
	sent, err := model.ParseTrafficCalibrationBytes(body.TargetSent)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid target_sent")
		return trafficCalibrationPayload{}, false
	}
	received, err := model.ParseTrafficCalibrationBytes(body.TargetReceived)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid target_received")
		return trafficCalibrationPayload{}, false
	}
	return trafficCalibrationPayload{request: body, sent: sent, received: received}, true
}

func decodeRevokePayload(w http.ResponseWriter, r *http.Request) (trafficCalibrationRevokeRequest, bool) {
	var body trafficCalibrationRevokeRequest
	if err := ParseJSONStrict(r, &body); err != nil {
		WriteBodyError(w, err)
		return trafficCalibrationRevokeRequest{}, false
	}
	return body, true
}

func calibrationModelRequest(payload trafficCalibrationPayload) model.TrafficCalibrationRequest {
	return model.TrafficCalibrationRequest{
		RequestID: payload.request.RequestID, EffectiveAt: payload.request.EffectiveAt,
		TargetSent: payload.sent, TargetReceived: payload.received, Reason: payload.request.Reason}
}

func calibrationActor(r *http.Request, requestID string) model.TrafficCalibrationActor {
	principal, _ := middleware.AdminPrincipal(r.Context())
	return model.TrafficCalibrationActor{
		ID: principal.User.ID, Name: principal.User.Username,
		SessionID: principal.Session.ID, RequestID: requestID}
}

func calibrationChangeResponse(change store.TrafficCalibrationChange) trafficCalibrationResponse {
	response := trafficCalibrationResponse{Event: newTrafficCalibrationView(change.Event), Replayed: change.Replayed}
	if change.Revoked != nil {
		view := newTrafficCalibrationView(*change.Revoked)
		response.Revoked = &view
	}
	return response
}

func calibrationHistoryLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, model.ErrTrafficCalibrationInvalid
	}
	return limit, nil
}

func (h *TrafficCalibrationHandler) respondError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if respondMetadataMutationError(w, err) {
		return true
	}
	switch {
	case errors.Is(err, model.ErrTrafficCalibrationInvalid):
		JSONError(w, http.StatusBadRequest, "invalid traffic calibration")
	case errors.Is(err, model.ErrTrafficCalibrationConflict):
		JSONError(w, http.StatusConflict, "traffic calibration conflict")
	case errors.Is(err, model.ErrTrafficCalibrationMissing):
		JSONError(w, http.StatusNotFound, "traffic calibration not found")
	default:
		JSONError(w, http.StatusInternalServerError, "traffic calibration failed")
	}
	return true
}
