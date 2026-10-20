package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// 拒绝分类会写进审计详情，因此必须是有限集合，不能回显错误原文。
const (
	calibrationRejectInvalid  = "invalid"
	calibrationRejectConflict = "conflict"
	calibrationRejectMissing  = "missing"
	calibrationRejectFailed   = "failed"
)

// respondRejected 记录失败审计后再写出错误响应。
//
// 审计写入失败只记日志：它不能把 400/409 变成 500，也不能掩盖原始的拒绝原因。
// 成功路径的审计仍由账本事务负责，写入失败会回滚整个追加。
func (h *TrafficCalibrationHandler) respondRejected(
	w http.ResponseWriter,
	r *http.Request,
	node model.Node,
	err error,
) bool {
	if err == nil {
		return false
	}
	h.recordRejection(r, node, calibrationRejectionReason(err))
	return h.respondError(w, err)
}

func (h *TrafficCalibrationHandler) recordRejection(r *http.Request, node model.Node, reason string) {
	if h == nil || h.ledger == nil {
		return
	}
	err := h.ledger.RecordRejection(context.WithoutCancel(r.Context()), store.TrafficCalibrationRejection{
		NodeID: node.ID, NodeName: node.Name, Reason: reason})
	if err != nil {
		slog.ErrorContext(r.Context(), "traffic calibration rejection audit failed", "error", err)
	}
}

func calibrationRejectionReason(err error) string {
	switch {
	case errors.Is(err, model.ErrTrafficCalibrationInvalid):
		return calibrationRejectInvalid
	case errors.Is(err, model.ErrTrafficCalibrationConflict):
		return calibrationRejectConflict
	case errors.Is(err, model.ErrTrafficCalibrationMissing):
		return calibrationRejectMissing
	default:
		return calibrationRejectFailed
	}
}
