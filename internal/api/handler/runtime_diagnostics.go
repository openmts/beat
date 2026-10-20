package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/runtimediag"
)

const runtimeCaptureBodyBytes = 16 << 10

// RuntimeDiagnostics 是所有者可见的运行时诊断能力；实现由 runtimediag.Service 提供。
type RuntimeDiagnostics interface {
	Summary() runtimediag.Summary
	List(ownerID string) []runtimediag.Capture
	Busy() bool
	Start(ctx context.Context, ownerID, sessionID, kind string, duration time.Duration) (runtimediag.Capture, error)
	Download(ownerID, id string) (io.ReadCloser, runtimediag.Capture, error)
	Cancel(ownerID, id string) error
	RevokeSession(sessionID string)
}

// RuntimeAudit 记录固定的审计动作；nil 时跳过审计。
type RuntimeAudit func(request *http.Request, action, resourceID string)

// RuntimeDiagnosticsHandler 暴露摘要、捕获、下载与取消；路由层已强制 owner 会话。
type RuntimeDiagnosticsHandler struct {
	service RuntimeDiagnostics
	audit   RuntimeAudit
}

func NewRuntimeDiagnosticsHandler(service RuntimeDiagnostics, audit RuntimeAudit) *RuntimeDiagnosticsHandler {
	return &RuntimeDiagnosticsHandler{service: service, audit: audit}
}

func (h *RuntimeDiagnosticsHandler) HandleSummary(w http.ResponseWriter, request *http.Request) {
	_, ok := runtimePrincipal(request)
	if !ok {
		JSONError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	JSONResponse(w, http.StatusOK, h.service.Summary())
}

func (h *RuntimeDiagnosticsHandler) HandleList(w http.ResponseWriter, request *http.Request) {
	principal, ok := runtimePrincipal(request)
	if !ok {
		JSONError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	JSONResponse(w, http.StatusOK, map[string]any{"captures": h.service.List(principal.User.ID), "busy": h.service.Busy()})
}

func (h *RuntimeDiagnosticsHandler) HandleCreate(w http.ResponseWriter, request *http.Request) {
	principal, ok := runtimePrincipal(request)
	if !ok {
		JSONError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	var body struct {
		Kind            string `json:"kind"`
		DurationSeconds int    `json:"duration_seconds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, runtimeCaptureBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		JSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	duration := time.Duration(body.DurationSeconds) * time.Second
	capture, err := h.service.Start(request.Context(), principal.User.ID, principal.Session.ID, body.Kind, duration)
	if err != nil {
		writeRuntimeDiagnosticsError(w, err)
		return
	}
	h.record(request, "runtime.capture.start", capture.ID)
	JSONResponse(w, http.StatusAccepted, capture)
}

func (h *RuntimeDiagnosticsHandler) HandleDownload(w http.ResponseWriter, request *http.Request) {
	principal, ok := runtimePrincipal(request)
	if !ok {
		JSONError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	reader, metadata, err := h.service.Download(principal.User.ID, request.PathValue("id"))
	if err != nil {
		writeRuntimeDiagnosticsError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.Bytes, 10))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+runtimeFileName(metadata)+"\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, reader)
	complete := copyErr == nil
	if err := reader.Close(); err != nil {
		complete = false
	}
	if complete {
		h.record(request, "runtime.capture.download", metadata.ID)
	}
}

func (h *RuntimeDiagnosticsHandler) HandleDelete(w http.ResponseWriter, request *http.Request) {
	principal, ok := runtimePrincipal(request)
	if !ok {
		JSONError(w, http.StatusForbidden, "owner permission is required")
		return
	}
	id := request.PathValue("id")
	if err := h.service.Cancel(principal.User.ID, id); err != nil {
		writeRuntimeDiagnosticsError(w, err)
		return
	}
	h.record(request, "runtime.capture.cancel", id)
	JSONResponse(w, http.StatusOK, map[string]string{"id": id})
}

func (h *RuntimeDiagnosticsHandler) record(request *http.Request, action, resourceID string) {
	if h.audit == nil {
		return
	}
	h.audit(request, action, resourceID)
}

func runtimePrincipal(request *http.Request) (model.AdminPrincipal, bool) {
	return middleware.AdminPrincipal(request.Context())
}

// runtimeFileName 只使用固定类型与十六进制标识构造下载名，避免响应头注入。
func runtimeFileName(metadata runtimediag.Capture) string {
	if !runtimediag.KindIsSupported(metadata.Kind) || !isHexIdentifier(metadata.ID) {
		return "capture.prof"
	}
	return metadata.Kind + "-" + metadata.ID + ".prof"
}

func isHexIdentifier(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	return strings.Trim(value, "0123456789abcdef") == ""
}

func writeRuntimeDiagnosticsError(w http.ResponseWriter, err error) {
	status, message := runtimeErrorStatus(runtimediag.CodeOf(err))
	JSONError(w, status, message)
}

func runtimeErrorStatus(code string) (int, string) {
	switch code {
	case runtimediag.CodeInvalidKind:
		return http.StatusBadRequest, "unsupported capture kind"
	case runtimediag.CodeInvalidDuration:
		return http.StatusBadRequest, "invalid capture duration"
	case runtimediag.CodeConflict:
		return http.StatusConflict, "another capture is already running"
	case runtimediag.CodeRateLimited:
		return http.StatusTooManyRequests, "capture rate limit exceeded"
	case runtimediag.CodeCapacity:
		return http.StatusServiceUnavailable, "runtime diagnostics is temporarily unavailable"
	case runtimediag.CodeNotFound:
		return http.StatusNotFound, "capture not found"
	default:
		return http.StatusInternalServerError, "runtime diagnostics failed"
	}
}
