package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type BackupOperations interface {
	Start(context.Context) (model.BackupRecord, error)
	List(context.Context, store.BackupQuery) (model.BackupPage, error)
	ListQuarantine(context.Context) ([]model.BackupQuarantineRecord, error)
	ListQuarantinedUploads(context.Context) ([]model.BackupUploadSession, error)
	Open(context.Context, string) (*os.File, model.BackupRecord, error)
	Delete(context.Context, string, backup.Actor) error
	ValidateUpload(context.Context, io.Reader) (model.BackupRecord, error)
	StageRestore(context.Context, backup.StageRequest) (model.BackupRecord, error)
	StartUpload(context.Context, store.BackupUploadActor, backup.UploadSessionRequest) (backup.UploadSessionStatus, error)
	ListUploads(context.Context, store.BackupUploadActor) ([]model.BackupUploadSession, error)
	UploadStatus(context.Context, store.BackupUploadActor, string) (backup.UploadSessionStatus, error)
	UploadChunk(context.Context, store.BackupUploadActor, string, backup.UploadChunkRequest) (model.BackupUploadChunk, error)
	FinalizeUpload(context.Context, store.BackupUploadActor, string) (model.BackupRecord, error)
	CancelUpload(context.Context, store.BackupUploadActor, string) error
}

type BackupHandler struct {
	operations BackupOperations
	security   *adminauth.Service
}

func NewBackupHandler(operations BackupOperations, security *adminauth.Service) *BackupHandler {
	return &BackupHandler{operations: operations, security: security}
}

func (handler *BackupHandler) HandleList(w http.ResponseWriter, request *http.Request) {
	if !handler.authorized(w, request, false) {
		return
	}
	query, err := backupQuery(request)
	if err != nil {
		JSONError(w, http.StatusBadRequest, "invalid backup query")
		return
	}
	page, err := handler.operations.List(request.Context(), query)
	if errors.Is(err, store.ErrInvalidBackupQuery) {
		JSONError(w, http.StatusBadRequest, "invalid backup query")
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "backups are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, page)
}

// backupQuery 只接受有界 limit 与不透明游标；非法取值一律 400。
func backupQuery(request *http.Request) (store.BackupQuery, error) {
	values := request.URL.Query()
	query := store.BackupQuery{State: values.Get("state"), Cursor: values.Get("cursor")}
	raw := values.Get("limit")
	if raw == "" {
		return query, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > store.BackupMaximumPage {
		return store.BackupQuery{}, store.ErrInvalidBackupQuery
	}
	query.Limit = limit
	return query, nil
}

// backupActor 把管理员主体与请求 ID 绑定到一次备份状态变更，供审计归因。
func backupActor(principal model.AdminPrincipal, request *http.Request) backup.Actor {
	return backup.Actor{ID: principal.User.ID, Username: principal.User.Username,
		Type: model.AuditActorAdmin, RequestID: middleware.RequestID(request.Context())}
}

// quarantineResponse 保留既有的 items/total 字段，并补上被拒分块上传会话的只读清单：
// 过去会话被拒后只能查库或翻自己的列表，运维看不到全貌。
type quarantineResponse struct {
	Items               []model.BackupQuarantineRecord `json:"items"`
	Total               int                            `json:"total"`
	UploadSessions      []model.BackupUploadSession    `json:"upload_sessions"`
	UploadSessionsTotal int                            `json:"upload_sessions_total"`
}

// HandleQuarantine 返回私有隔离清单，让运维看到数量而看不到服务器路径。
func (handler *BackupHandler) HandleQuarantine(w http.ResponseWriter, request *http.Request) {
	if !handler.authorized(w, request, false) {
		return
	}
	records, err := handler.operations.ListQuarantine(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "backups are unavailable")
		return
	}
	sessions, err := handler.operations.ListQuarantinedUploads(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "backups are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, quarantineResponse{Items: records, Total: len(records),
		UploadSessions: sessions, UploadSessionsTotal: len(sessions)})
}

func (handler *BackupHandler) HandleCreate(w http.ResponseWriter, request *http.Request) {
	if !handler.authorized(w, request, false) {
		return
	}
	record, err := handler.operations.Start(request.Context())
	if errors.Is(err, backup.ErrAlreadyRunning) {
		JSONError(w, http.StatusConflict, "a backup is already running")
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "backup could not be started")
		return
	}
	JSONResponse(w, http.StatusAccepted, record)
}

func (handler *BackupHandler) HandleDownload(w http.ResponseWriter, request *http.Request) {
	if !handler.authorized(w, request, true) {
		return
	}
	file, record, err := handler.operations.Open(request.Context(), request.PathValue("id"))
	if err != nil {
		handler.writeError(w, err)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "backup download is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(record.Filename)))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, request, record.Filename, info.ModTime(), file)
}

func (handler *BackupHandler) HandleDelete(w http.ResponseWriter, request *http.Request) {
	principal, ok := handler.requireOwner(w, request, false)
	if !ok {
		return
	}
	actor := backupActor(principal, request)
	if err := handler.operations.Delete(request.Context(), request.PathValue("id"), actor); err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *BackupHandler) HandleValidate(w http.ResponseWriter, request *http.Request) {
	if _, ok := handler.requireOwner(w, request, false); !ok {
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, backup.MaximumUploadBytes)
	record, err := handler.operations.ValidateUpload(request.Context(), request.Body)
	if err != nil {
		slog.WarnContext(request.Context(), "backup archive validation failed",
			"request_id", middleware.RequestID(request.Context()), "error", err)
		JSONError(w, http.StatusBadRequest, "backup archive validation failed")
		return
	}
	JSONResponse(w, http.StatusCreated, record)
}

func (handler *BackupHandler) HandleStage(w http.ResponseWriter, request *http.Request) {
	principal, ok := handler.requireOwner(w, request, true)
	if !ok {
		return
	}
	var body struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	record, err := handler.operations.StageRestore(request.Context(), backup.StageRequest{
		BackupID: request.PathValue("id"), Confirmation: body.Confirmation,
		Actor: backupActor(principal, request)})
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, record)
}

// authorized 对已通过中间件 scope 与资源校验的服务主体跳过浏览器 owner 检查；
// 其余调用方仍走既有的 owner 与近期重认证门禁。
func (handler *BackupHandler) authorized(w http.ResponseWriter, request *http.Request, recent bool) bool {
	if _, ok := middleware.ServicePrincipalFrom(request.Context()); ok {
		return true
	}
	_, ok := handler.requireOwner(w, request, recent)
	return ok
}

func (handler *BackupHandler) requireOwner(
	w http.ResponseWriter,
	request *http.Request,
	recent bool,
) (model.AdminPrincipal, bool) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return model.AdminPrincipal{}, false
	}
	var err error
	if recent {
		err = handler.security.RequireOwnerRecent(&principal)
	} else {
		err = handler.security.RequireOwner(&principal)
	}
	if errors.Is(err, adminauth.ErrRecentAuthRequired) {
		JSONError(w, http.StatusPreconditionRequired, "recent authentication is required")
		return model.AdminPrincipal{}, false
	}
	if err != nil {
		JSONError(w, http.StatusForbidden, "owner access is required")
		return model.AdminPrincipal{}, false
	}
	return principal, true
}

func (handler *BackupHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		JSONError(w, http.StatusNotFound, "backup was not found")
	case errors.Is(err, backup.ErrInvalidConfirm):
		JSONError(w, http.StatusBadRequest, "restore confirmation phrase is invalid")
	case errors.Is(err, backup.ErrRestoreStaged):
		JSONError(w, http.StatusConflict, "another restore is already staged")
	case errors.Is(err, store.ErrInvalidBackupQuery):
		JSONError(w, http.StatusBadRequest, "invalid backup query")
	default:
		JSONError(w, http.StatusBadRequest, "backup operation failed")
	}
}
