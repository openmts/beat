package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/adminauth"
	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/store"
)

type fakeBackupOperations struct {
	filePath    string
	startErr    error
	listErr     error
	openErr     error
	deleteErr   error
	validateErr error
	stageErr    error
	closeOpen   bool
	listed      bool
	staged      bool
}

func (fake *fakeBackupOperations) Start(context.Context) (model.BackupRecord, error) {
	return model.BackupRecord{ID: "backup", State: model.BackupStateRunning}, fake.startErr
}

func (fake *fakeBackupOperations) List(context.Context) ([]model.BackupRecord, error) {
	fake.listed = true
	return []model.BackupRecord{{ID: "backup", State: model.BackupStateReady}}, fake.listErr
}

func (fake *fakeBackupOperations) Open(
	context.Context,
	string,
) (*os.File, model.BackupRecord, error) {
	if fake.openErr != nil {
		return nil, model.BackupRecord{}, fake.openErr
	}
	file, err := os.Open(fake.filePath)
	if err == nil && fake.closeOpen {
		err = file.Close()
	}
	return file, model.BackupRecord{ID: "backup", Filename: "backup.zip", State: model.BackupStateReady}, err
}

func (fake *fakeBackupOperations) Delete(context.Context, string) error { return fake.deleteErr }

func (fake *fakeBackupOperations) ValidateUpload(
	context.Context,
	io.Reader,
) (model.BackupRecord, error) {
	return model.BackupRecord{ID: "upload", State: model.BackupStateValidated}, fake.validateErr
}

func (fake *fakeBackupOperations) StageRestore(
	context.Context,
	string,
	string,
) (model.BackupRecord, error) {
	fake.staged = true
	return model.BackupRecord{ID: "backup", State: model.BackupStateStaged}, fake.stageErr
}

func TestBackupHandlerOwnerAuthorizationAndRecentAuthentication(t *testing.T) {
	handler, operations := newTestBackupHandler(t)
	tests := []struct {
		name      string
		principal *model.AdminPrincipal
		status    int
	}{
		{name: "unauthenticated", status: http.StatusUnauthorized},
		{name: "administrator", principal: backupPrincipal(model.AdminRoleAdmin, true), status: http.StatusForbidden},
		{name: "owner", principal: backupPrincipal(model.AdminRoleOwner, true), status: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backups", nil)
			if test.principal != nil {
				request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), *test.principal))
			}
			response := httptest.NewRecorder()
			handler.HandleList(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
	if !operations.listed {
		t.Fatal("owner request did not reach backup operations")
	}

	notRecent := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backups/backup/download", nil)
	notRecent.SetPathValue("id", "backup")
	principal := backupPrincipal(model.AdminRoleOwner, false)
	notRecent = notRecent.WithContext(middleware.WithAdminPrincipal(notRecent.Context(), *principal))
	response := httptest.NewRecorder()
	handler.HandleDownload(response, notRecent)
	if response.Code != http.StatusPreconditionRequired {
		t.Fatalf("download status = %d, want %d", response.Code, http.StatusPreconditionRequired)
	}

	recent := httptest.NewRequest(http.MethodPost, "/api/v1/admin/backups/backup/stage-restore",
		strings.NewReader(`{"confirmation":"RESTORE BEAT"}`))
	recent.SetPathValue("id", "backup")
	recent = recent.WithContext(middleware.WithAdminPrincipal(recent.Context(), *backupPrincipal(model.AdminRoleOwner, true)))
	response = httptest.NewRecorder()
	handler.HandleStage(response, recent)
	if response.Code != http.StatusOK || !operations.staged {
		t.Fatalf("stage status = %d, staged = %v", response.Code, operations.staged)
	}
}

func TestBackupHandlerOperationsAndErrors(t *testing.T) {
	handler, operations := newTestBackupHandler(t)
	owner := backupPrincipal(model.AdminRoleOwner, true)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		handle     func(http.ResponseWriter, *http.Request)
		configure  func()
		wantStatus int
	}{
		{name: "create", method: http.MethodPost, path: "/api/v1/admin/backups", handle: handler.HandleCreate,
			configure: func() { operations.startErr = nil }, wantStatus: http.StatusAccepted},
		{name: "create conflict", method: http.MethodPost, path: "/api/v1/admin/backups", handle: handler.HandleCreate,
			configure: func() { operations.startErr = backup.ErrAlreadyRunning }, wantStatus: http.StatusConflict},
		{name: "create failure", method: http.MethodPost, path: "/api/v1/admin/backups", handle: handler.HandleCreate,
			configure: func() { operations.startErr = errors.New("start") }, wantStatus: http.StatusInternalServerError},
		{name: "list failure", method: http.MethodGet, path: "/api/v1/admin/backups", handle: handler.HandleList,
			configure: func() { operations.listErr = errors.New("list") }, wantStatus: http.StatusInternalServerError},
		{name: "validate", method: http.MethodPost, path: "/api/v1/admin/backups/validate", body: "archive",
			handle: handler.HandleValidate, configure: func() { operations.validateErr = nil }, wantStatus: http.StatusCreated},
		{name: "validate failure", method: http.MethodPost, path: "/api/v1/admin/backups/validate", body: "bad",
			handle: handler.HandleValidate, configure: func() { operations.validateErr = errors.New("invalid") },
			wantStatus: http.StatusBadRequest},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/admin/backups/backup", handle: handler.HandleDelete,
			configure: func() { operations.deleteErr = nil }, wantStatus: http.StatusOK},
		{name: "delete missing", method: http.MethodDelete, path: "/api/v1/admin/backups/missing", handle: handler.HandleDelete,
			configure: func() { operations.deleteErr = backup.ErrNotFound }, wantStatus: http.StatusNotFound},
		{name: "delete failure", method: http.MethodDelete, path: "/api/v1/admin/backups/backup", handle: handler.HandleDelete,
			configure: func() { operations.deleteErr = errors.New("delete") }, wantStatus: http.StatusBadRequest},
		{name: "stage invalid body", method: http.MethodPost, path: "/api/v1/admin/backups/backup/stage-restore",
			body: "{", handle: handler.HandleStage, configure: func() { operations.stageErr = nil },
			wantStatus: http.StatusBadRequest},
		{name: "stage invalid confirmation", method: http.MethodPost, path: "/api/v1/admin/backups/backup/stage-restore",
			body: `{"confirmation":"wrong"}`, handle: handler.HandleStage,
			configure: func() { operations.stageErr = backup.ErrInvalidConfirm }, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetBackupOperationErrors(operations)
			test.configure()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.SetPathValue("id", "backup")
			request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), *owner))
			response := httptest.NewRecorder()
			test.handle(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestBackupHandlerDownloadPaths(t *testing.T) {
	handler, operations := newTestBackupHandler(t)
	owner := backupPrincipal(model.AdminRoleOwner, true)
	tests := []struct {
		name       string
		configure  func()
		wantStatus int
	}{
		{name: "download", configure: func() {}, wantStatus: http.StatusOK},
		{name: "missing", configure: func() { operations.openErr = backup.ErrNotFound }, wantStatus: http.StatusNotFound},
		{name: "open failure", configure: func() { operations.openErr = errors.New("open") }, wantStatus: http.StatusBadRequest},
		{name: "stat failure", configure: func() { operations.closeOpen = true }, wantStatus: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetBackupOperationErrors(operations)
			test.configure()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backups/backup/download", nil)
			request.SetPathValue("id", "backup")
			request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), *owner))
			response := httptest.NewRecorder()
			handler.HandleDownload(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/backups/backup/download", nil)
	adminRequest = adminRequest.WithContext(middleware.WithAdminPrincipal(
		adminRequest.Context(), *backupPrincipal(model.AdminRoleAdmin, true),
	))
	response := httptest.NewRecorder()
	handler.HandleDownload(response, adminRequest)
	if response.Code != http.StatusForbidden {
		t.Fatalf("administrator download status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func resetBackupOperationErrors(operations *fakeBackupOperations) {
	operations.startErr = nil
	operations.listErr = nil
	operations.openErr = nil
	operations.deleteErr = nil
	operations.validateErr = nil
	operations.stageErr = nil
	operations.closeOpen = false
}

func newTestBackupHandler(t *testing.T) (*BackupHandler, *fakeBackupOperations) {
	t.Helper()
	root := t.TempDir()
	sqliteStore, err := store.NewSQLiteStore(filepath.Join(root, "beat.db"))
	if err != nil {
		t.Fatalf("new SQLite store: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	secrets, err := secretbox.New(filepath.Join(root, "admin-data.key"), nil)
	if err != nil {
		t.Fatalf("new secret box: %v", err)
	}
	security, err := adminauth.NewService(adminauth.ServiceConfig{
		Store: store.NewAdminStore(sqliteStore.DB), Secrets: secrets, LegacyToken: "token",
	})
	if err != nil {
		t.Fatalf("new administrator security: %v", err)
	}
	filePath := filepath.Join(root, "backup.zip")
	if err := os.WriteFile(filePath, []byte("zip"), 0o600); err != nil {
		t.Fatalf("write fake backup: %v", err)
	}
	operations := &fakeBackupOperations{filePath: filePath}
	return NewBackupHandler(operations, security), operations
}

func backupPrincipal(role model.AdminRole, recent bool) *model.AdminPrincipal {
	principal := &model.AdminPrincipal{User: model.AdminUser{ID: "user", Role: role, Enabled: true},
		Session: model.AdminSession{ID: "session"}}
	if recent {
		until := time.Now().UTC().Add(time.Minute)
		principal.Session.ReauthenticatedUntil = &until
	}
	return principal
}
