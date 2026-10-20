package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

// 分块上传使用的请求头，与远程操作分块传输保持同一约定。
const (
	chunkIndexHeader  = "X-Beat-Chunk-Index"
	chunkRangeHeader  = "Content-Range"
	chunkDigestHeader = "X-Content-SHA256"
)

type BackupUploadHandler struct {
	operations BackupOperations
	security   *adminauth.Service
}

func NewBackupUploadHandler(
	operations BackupOperations,
	security *adminauth.Service,
) *BackupUploadHandler {
	return &BackupUploadHandler{operations: operations, security: security}
}

// uploadSessionView 暴露会话进度、已收分块索引与可并发分块数。
type uploadSessionView struct {
	model.BackupUploadSession
	MaximumParallelChunks int   `json:"maximum_parallel_chunks"`
	ReceivedIndexes       []int `json:"received_indexes"`
}

func (handler *BackupUploadHandler) HandleStart(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.uploadActor(w, request, true)
	if !ok {
		return
	}
	var body struct {
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
		Digest    string `json:"digest"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		WriteBodyError(w, err)
		return
	}
	status, err := handler.operations.StartUpload(request.Context(), actor,
		backup.UploadSessionRequest{Filename: body.Filename, SizeBytes: body.SizeBytes,
			Digest: body.Digest})
	if err != nil {
		handler.writeUploadError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, uploadSessionView{BackupUploadSession: status.Session,
		ReceivedIndexes:       status.ReceivedIndexes,
		MaximumParallelChunks: model.MaximumParallelBackupUploadChunks})
}

func (handler *BackupUploadHandler) HandleList(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.uploadActor(w, request, false)
	if !ok {
		return
	}
	sessions, err := handler.operations.ListUploads(request.Context(), actor)
	if err != nil {
		handler.writeUploadError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, sessions)
}

func (handler *BackupUploadHandler) HandleStatus(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.uploadActor(w, request, false)
	if !ok {
		return
	}
	status, err := handler.operations.UploadStatus(request.Context(), actor,
		request.PathValue("session"))
	if err != nil {
		handler.writeUploadError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, uploadSessionView{BackupUploadSession: status.Session,
		ReceivedIndexes:       status.ReceivedIndexes,
		MaximumParallelChunks: model.MaximumParallelBackupUploadChunks})
}

func (handler *BackupUploadHandler) HandleChunk(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.uploadActor(w, request, false)
	if !ok {
		return
	}
	chunk, ok := uploadChunkRequest(w, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, model.MaximumBackupUploadChunkBytes+1)
	chunk.Body = request.Body
	stored, err := handler.operations.UploadChunk(request.Context(), actor,
		request.PathValue("session"), chunk)
	if err != nil {
		handler.writeUploadError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, stored)
}

func (handler *BackupUploadHandler) HandleFinalize(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.uploadActor(w, request, true)
	if !ok {
		return
	}
	record, err := handler.operations.FinalizeUpload(request.Context(), actor,
		request.PathValue("session"))
	if err != nil {
		handler.writeUploadError(w, err)
		return
	}
	JSONResponse(w, http.StatusCreated, record)
}

func (handler *BackupUploadHandler) HandleCancel(w http.ResponseWriter, request *http.Request) {
	actor, ok := handler.uploadActor(w, request, false)
	if !ok {
		return
	}
	if err := handler.operations.CancelUpload(request.Context(), actor,
		request.PathValue("session")); err != nil {
		handler.writeUploadError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, struct{}{})
}

func (handler *BackupUploadHandler) uploadActor(
	w http.ResponseWriter,
	request *http.Request,
	recent bool,
) (store.BackupUploadActor, bool) {
	principal, ok := middleware.AdminPrincipal(request.Context())
	if !ok || principal.User.ID == "" {
		JSONError(w, http.StatusUnauthorized, "unauthorized")
		return store.BackupUploadActor{}, false
	}
	var err error
	if recent {
		err = handler.security.RequireOwnerRecent(&principal)
	} else {
		err = handler.security.RequireOwner(&principal)
	}
	if errors.Is(err, adminauth.ErrRecentAuthRequired) {
		JSONError(w, http.StatusPreconditionRequired, "recent authentication is required")
		return store.BackupUploadActor{}, false
	}
	if err != nil {
		JSONError(w, http.StatusForbidden, "owner access is required")
		return store.BackupUploadActor{}, false
	}
	return store.BackupUploadActor{UserID: principal.User.ID, SessionID: principal.Session.ID,
		RequestID: middleware.RequestID(request.Context()), IPAddress: middleware.ClientIP(request),
		UserAgent: request.UserAgent(), Username: principal.User.Username,
		SessionPrefix: principal.Session.TokenPrefix}, true
}

func uploadChunkRequest(w http.ResponseWriter, request *http.Request) (backup.UploadChunkRequest, bool) {
	index, indexErr := strconv.Atoi(request.Header.Get(chunkIndexHeader))
	start, end, rangeErr := parseByteRange(request.Header.Get(chunkRangeHeader))
	if indexErr != nil || rangeErr != nil || end < start {
		JSONError(w, http.StatusBadRequest, "chunk range headers are invalid")
		return backup.UploadChunkRequest{}, false
	}
	return backup.UploadChunkRequest{Index: index, Offset: start, SizeBytes: end - start + 1,
		Digest: request.Header.Get(chunkDigestHeader)}, true
}

// parseByteRange 解析 `bytes <start>-<end>/<total>` 形式的分块范围。
func parseByteRange(value string) (int64, int64, error) {
	trimmed, ok := strings.CutPrefix(value, "bytes ")
	if !ok {
		return 0, 0, errors.New("chunk range is invalid")
	}
	rangePart, _, ok := strings.Cut(trimmed, "/")
	if !ok {
		return 0, 0, errors.New("chunk range is invalid")
	}
	startPart, endPart, ok := strings.Cut(rangePart, "-")
	if !ok {
		return 0, 0, errors.New("chunk range is invalid")
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startPart), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.ParseInt(strings.TrimSpace(endPart), 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func (handler *BackupUploadHandler) writeUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrUploadNotFound):
		JSONError(w, http.StatusNotFound, "upload session was not found")
	case errors.Is(err, backup.ErrUploadLimit):
		JSONError(w, http.StatusConflict, "too many open upload sessions")
	case errors.Is(err, backup.ErrUploadClosed):
		JSONError(w, http.StatusConflict, "upload session is no longer open")
	case errors.Is(err, backup.ErrUploadUnavailable):
		JSONError(w, http.StatusServiceUnavailable, "resumable uploads are unavailable")
	case errors.Is(err, backup.ErrUploadFilename), errors.Is(err, backup.ErrUploadDigest),
		errors.Is(err, backup.ErrUploadChunkRange):
		JSONError(w, http.StatusBadRequest, "upload request is invalid")
	case errors.Is(err, backup.ErrUploadChunkDigest):
		JSONError(w, http.StatusUnprocessableEntity, "chunk digest does not match the bytes")
	case errors.Is(err, backup.ErrUploadChunkConflict):
		JSONError(w, http.StatusConflict, "chunk conflicts with an earlier attempt")
	case errors.Is(err, backup.ErrUploadIncomplete):
		JSONError(w, http.StatusConflict, "upload is missing chunks")
	default:
		JSONError(w, http.StatusBadRequest, "upload request failed")
	}
}
