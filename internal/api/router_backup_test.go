package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/beat/backend/internal/api/handler"
	"github.com/beat/backend/internal/model"
)

type optionBackup struct{}

func (optionBackup) Start(context.Context) (model.BackupRecord, error) {
	return model.BackupRecord{ID: "backup", State: model.BackupStateRunning}, nil
}

func (optionBackup) List(context.Context) ([]model.BackupRecord, error) {
	return []model.BackupRecord{{ID: "backup", State: model.BackupStateReady}}, nil
}

func (optionBackup) Open(context.Context, string) (*os.File, model.BackupRecord, error) {
	return nil, model.BackupRecord{ID: "backup", Filename: "backup.zip"}, nil
}

func (optionBackup) Delete(context.Context, string) error { return nil }

func (optionBackup) ValidateUpload(context.Context, io.Reader) (model.BackupRecord, error) {
	return model.BackupRecord{ID: "upload", State: model.BackupStateValidated}, nil
}

func (optionBackup) StageRestore(context.Context, string, string) (model.BackupRecord, error) {
	return model.BackupRecord{ID: "backup", State: model.BackupStateStaged}, nil
}

var _ handler.BackupOperations = optionBackup{}

func setupSessionRouterWithBackup(t *testing.T) *Router {
	t.Helper()
	return setupSessionRouterWithOptions(t, WithBackupService(optionBackup{}))
}

func TestRouterWithBackupServiceRegistersRoutes(t *testing.T) {
	router := setupSessionRouterWithBackup(t)

	state := requestRouter(router, http.MethodGet, "/api/v1/auth/state", "", "", "")
	if state.Code != http.StatusOK {
		t.Fatalf("state status = %d", state.Code)
	}
	bootstrap := requestRouter(router, http.MethodPost, "/api/v1/auth/bootstrap",
		`{"bootstrap_token":"admin-secret","username":"owner","display_name":"Owner",`+
			`"password":"correct horse battery staple"}`, "", "http://example.com")
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}
	cookie := bootstrap.Result().Cookies()[0]

	list := requestRouterWithCookie(router, http.MethodGet, "/api/v1/admin/backups", "", cookie, "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"state":"ready"`) {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	create := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/backups", "",
		cookie, "http://example.com")
	if create.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	validate := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/backups/validate",
		"archive", cookie, "http://example.com")
	if validate.Code != http.StatusCreated {
		t.Fatalf("validate status = %d, body = %s", validate.Code, validate.Body.String())
	}
	deleteResponse := requestRouterWithCookie(router, http.MethodDelete, "/api/v1/admin/backups/backup",
		"", cookie, "http://example.com")
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate",
		`{"password":"correct horse battery staple","totp_code":""}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatalf("reauthenticate status = %d, body = %s", reauth.Code, reauth.Body.String())
	}
	stage := requestRouterWithCookie(router, http.MethodPost, "/api/v1/admin/backups/backup/stage-restore",
		`{"confirmation":"RESTORE BEAT"}`, cookie, "http://example.com")
	if stage.Code != http.StatusOK {
		t.Fatalf("stage status = %d, body = %s", stage.Code, stage.Body.String())
	}
}

func TestRouterWithBackupServiceUnauthenticated(t *testing.T) {
	router := setupSessionRouterWithBackup(t)

	list := requestRouter(router, http.MethodGet, "/api/v1/admin/backups", "", "", "")
	if list.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want 401", list.Code)
	}
}

func TestBodyNodeNameResolvers(t *testing.T) {
	resolver := bodyNodeName("name")
	valid := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"node-one"}`))
	name, ok := resolver(valid)
	if !ok || name != "node-one" {
		t.Fatalf("name = %q, ok = %v", name, ok)
	}
	if valid.Body == nil {
		t.Fatal("resolver consumed the request body")
	}
	for _, body := range []string{
		`{"name":""}`,
		`not-json`,
		`{"other":"value"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		if name, ok := resolver(request); ok || name != "" {
			t.Fatalf("body %q resolved to %q, %v", body, name, ok)
		}
	}
}
