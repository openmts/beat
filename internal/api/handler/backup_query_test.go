package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func backupRequest(t *testing.T, method, target, body string, principal model.AdminPrincipal) *http.Request {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(middleware.WithAdminPrincipal(request.Context(), principal))
	return request
}

func TestBackupListForwardsBoundedQueryAndRejectsInvalidLimits(t *testing.T) {
	handler, operations := newTestBackupHandler(t)
	request := backupRequest(t, http.MethodGet,
		"/api/v1/admin/backups?state=ready&cursor=abc&limit=25", "",
		*backupPrincipal(model.AdminRoleOwner, false))
	recorder := httptest.NewRecorder()
	handler.HandleList(recorder, request)
	if recorder.Code != http.StatusOK || !operations.listed {
		t.Fatalf("list status = %d, listed = %v", recorder.Code, operations.listed)
	}
	if operations.listQuery.State != "ready" || operations.listQuery.Cursor != "abc" ||
		operations.listQuery.Limit != 25 {
		t.Fatalf("forwarded query = %#v", operations.listQuery)
	}
	for _, limit := range []string{"0", "-1", "201", "abc"} {
		recorder = httptest.NewRecorder()
		handler.HandleList(recorder, backupRequest(t, http.MethodGet,
			"/api/v1/admin/backups?limit="+limit, "", *backupPrincipal(model.AdminRoleOwner, false)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("limit %s status = %d", limit, recorder.Code)
		}
	}
	operations.listErr = store.ErrInvalidBackupQuery
	recorder = httptest.NewRecorder()
	handler.HandleList(recorder, backupRequest(t, http.MethodGet, "/api/v1/admin/backups?cursor=bad",
		"", *backupPrincipal(model.AdminRoleOwner, false)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", recorder.Code)
	}
	operations.listErr = errors.New("unavailable")
	recorder = httptest.NewRecorder()
	handler.HandleList(recorder, backupRequest(t, http.MethodGet, "/api/v1/admin/backups", "",
		*backupPrincipal(model.AdminRoleOwner, false)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unavailable status = %d", recorder.Code)
	}
}

func TestBackupMutationsCarryAuditActorAndRestoreRequest(t *testing.T) {
	handler, operations := newTestBackupHandler(t)
	principal := *backupPrincipal(model.AdminRoleOwner, true)
	principal.User.Username = "owner"
	wrapped := middleware.RequestContext(middleware.TrustedProxies{})(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			handler.HandleDelete(writer, request)
		}))
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, backupRequest(t, http.MethodDelete, "/api/v1/admin/backups/backup", "", principal))
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d", recorder.Code)
	}
	if operations.deleteActor.ID != "user" || operations.deleteActor.Username != "owner" ||
		operations.deleteActor.Type != model.AuditActorAdmin || operations.deleteActor.RequestID == "" {
		t.Fatalf("delete actor = %#v", operations.deleteActor)
	}

	stageRequest := backupRequest(t, http.MethodPost, "/api/v1/admin/backups/backup/stage-restore",
		`{"confirmation":"RESTORE BEAT"}`, principal)
	stageRequest.SetPathValue("id", "backup")
	recorder = httptest.NewRecorder()
	handler.HandleStage(recorder, stageRequest)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stage status = %d", recorder.Code)
	}
	if operations.stageInput.BackupID != "backup" ||
		operations.stageInput.Confirmation != backup.RestoreConfirmation ||
		operations.stageInput.Actor.ID != "user" {
		t.Fatalf("stage request = %#v", operations.stageInput)
	}

	operations.stageErr = backup.ErrRestoreStaged
	conflictRequest := backupRequest(t, http.MethodPost,
		"/api/v1/admin/backups/backup/stage-restore", `{"confirmation":"RESTORE BEAT"}`, principal)
	conflictRequest.SetPathValue("id", "backup")
	recorder = httptest.NewRecorder()
	handler.HandleStage(recorder, conflictRequest)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("conflicting stage status = %d", recorder.Code)
	}
}

func TestBackupQuarantineRequiresOwnerAndReturnsInventory(t *testing.T) {
	handler, operations := newTestBackupHandler(t)
	recorder := httptest.NewRecorder()
	handler.HandleQuarantine(recorder, backupRequest(t, http.MethodGet,
		"/api/v1/admin/backups/quarantine", "", *backupPrincipal(model.AdminRoleAdmin, false)))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("administrator quarantine status = %d", recorder.Code)
	}
	operations.listErr = errors.New("unavailable")
	recorder = httptest.NewRecorder()
	handler.HandleQuarantine(recorder, backupRequest(t, http.MethodGet,
		"/api/v1/admin/backups/quarantine", "", *backupPrincipal(model.AdminRoleOwner, false)))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unavailable quarantine status = %d", recorder.Code)
	}
	operations.listErr = nil
	recorder = httptest.NewRecorder()
	handler.HandleQuarantine(recorder, backupRequest(t, http.MethodGet,
		"/api/v1/admin/backups/quarantine", "", *backupPrincipal(model.AdminRoleOwner, false)))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"total":0`) {
		t.Fatalf("quarantine response = %d, %s", recorder.Code, recorder.Body.String())
	}
}
