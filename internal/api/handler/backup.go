package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
)

type BackupOperations interface {
	Start(context.Context) (model.BackupRecord, error)
	List(context.Context) ([]model.BackupRecord, error)
	Open(context.Context, string) (*os.File, model.BackupRecord, error)
	Delete(context.Context, string) error
	ValidateUpload(context.Context, io.Reader) (model.BackupRecord, error)
	StageRestore(context.Context, string, string) (model.BackupRecord, error)
}

type BackupHandler struct {
	operations BackupOperations
	security   *adminauth.Service
}

func NewBackupHandler(operations BackupOperations, security *adminauth.Service) *BackupHandler {
	return &BackupHandler{operations: operations, security: security}
}

func (handler *BackupHandler) HandleList(w http.ResponseWriter, request *http.Request) {
	if _, ok := handler.requireOwner(w, request, false); !ok {
		return
	}
	records, err := handler.operations.List(request.Context())
	if err != nil {
		JSONError(w, http.StatusInternalServerError, "backups are unavailable")
		return
	}
	JSONResponse(w, http.StatusOK, records)
}

func (handler *BackupHandler) HandleCreate(w http.ResponseWriter, request *http.Request) {
	if _, ok := handler.requireOwner(w, request, false); !ok {
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
	if _, ok := handler.requireOwner(w, request, true); !ok {
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
	if _, ok := handler.requireOwner(w, request, false); !ok {
		return
	}
	if err := handler.operations.Delete(request.Context(), request.PathValue("id")); err != nil {
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
		JSONError(w, http.StatusBadRequest, "backup archive validation failed")
		return
	}
	JSONResponse(w, http.StatusCreated, record)
}

func (handler *BackupHandler) HandleStage(w http.ResponseWriter, request *http.Request) {
	if _, ok := handler.requireOwner(w, request, true); !ok {
		return
	}
	var body struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decodeAuthBody(w, request, &body); err != nil {
		JSONError(w, http.StatusBadRequest, "restore request is invalid")
		return
	}
	record, err := handler.operations.StageRestore(
		request.Context(), request.PathValue("id"), body.Confirmation,
	)
	if err != nil {
		handler.writeError(w, err)
		return
	}
	JSONResponse(w, http.StatusOK, record)
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
	default:
		JSONError(w, http.StatusBadRequest, "backup operation failed")
	}
}
