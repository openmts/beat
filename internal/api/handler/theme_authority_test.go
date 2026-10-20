package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestThemeWriteRechecksActualOwnerSessionInsideTransaction(t *testing.T) {
	cases := []struct {
		name, statement string
		status          int
	}{
		{"disabled", "UPDATE admin_users SET enabled=0", http.StatusForbidden},
		{"demoted", "UPDATE admin_users SET role='admin'", http.StatusForbidden},
		{"revoked", "UPDATE admin_sessions SET revoked_at='2000-01-01 00:00:00'", http.StatusForbidden},
		{"idle expired", "UPDATE admin_sessions SET idle_expires_at='2000-01-01 00:00:00'", http.StatusForbidden},
		{"absolute expired", "UPDATE admin_sessions SET absolute_expires_at='2000-01-01 00:00:00'", http.StatusForbidden},
		{"reauth missing", "UPDATE admin_sessions SET reauthenticated_until=NULL", http.StatusPreconditionRequired},
		{"reauth expired", "UPDATE admin_sessions SET reauthenticated_until='2000-01-01 00:00:00'", http.StatusPreconditionRequired},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			handler, service, database := setupThemeHandler(t)
			if _, err := database.DB.Exec(test.statement); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(themeUploadZip(t))).WithContext(themeOwnerContext().Context())
			response := httptest.NewRecorder()
			handler.HandleUpload(response, request)
			if response.Code != test.status {
				t.Fatalf("stale principal accepted: %d %s", response.Code, response.Body.String())
			}
			packages, err := service.ListPackages(t.Context())
			if err != nil || len(packages) != 0 {
				t.Fatalf("unauthorized package persisted: %+v %v", packages, err)
			}
			files, err := os.ReadDir(service.PackageDir(""))
			if err != nil || len(files) != 0 {
				t.Fatalf("unauthorized assets persisted: %+v %v", files, err)
			}
		})
	}
}

func TestThemeHTTPMutationsNeverFallBackToSystemAuthority(t *testing.T) {
	handler, _, _ := setupThemeHandler(t)
	for name, method := range map[string]http.HandlerFunc{
		"upload": handler.HandleUpload, "activate": handler.HandleActivate, "delete": handler.HandleDelete,
		"rollback": handler.HandleRollback, "built-in": handler.HandleBuiltIn, "configuration": handler.HandleConfiguration,
		"create source": handler.HandleCreateMarket, "update source": handler.HandleUpdateMarket,
		"delete source": handler.HandleDeleteMarket, "refresh catalog": handler.HandleRefreshCatalog, "install": handler.HandleInstallFromMarket,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/mutation", bytes.NewReader(themeUploadZip(t)))
			response := httptest.NewRecorder()
			method(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("missing session accepted: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestThemeResourceAuditUsesPersistedActorAndFailedAuditRollsBack(t *testing.T) {
	handler, service, database := setupThemeHandler(t)
	actor := store.ThemeActor{UserID: "owner-1", SessionID: "s-1", RequestID: "theme-audit-request", IPAddress: "127.0.0.1", UserAgent: "theme test"}
	ctx := store.WithThemeActor(t.Context(), actor)
	pkg, err := service.Install(ctx, themeUploadZip(t), model.ThemeSourceUpload, "", actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	var username, session, requestID string
	err = database.DB.QueryRow(`SELECT actor_username,session_prefix,request_id FROM admin_audit_events WHERE action='theme.install' AND resource_id=?`, pkg.ID).Scan(&username, &session, &requestID)
	if err != nil || username != "theme-owner" || session != "theme-fixture" || requestID != actor.RequestID {
		t.Fatalf("audit attribution: %s %s %s %v", username, session, requestID, err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER reject_theme_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit offline'); END`); err != nil {
		t.Fatal(err)
	}
	for name, method := range map[string]http.HandlerFunc{"activate": handler.HandleActivate, "configuration": handler.HandleConfiguration, "delete": handler.HandleDelete} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/mutation", strings.NewReader(`{"overview":"tiles"}`)).WithContext(themeOwnerContext().Context())
			request.SetPathValue("id", pkg.ID)
			response := httptest.NewRecorder()
			method(response, request)
			if response.Code >= 200 && response.Code < 300 {
				t.Fatalf("unaudited mutation succeeded: %d", response.Code)
			}
		})
	}
	stored, err := store.NewThemeStore(database.DB).GetPackageByID(t.Context(), pkg.ID)
	if err != nil || stored.State != model.ThemeStateInstalled || stored.ConfigurationJSON != "{}" {
		t.Fatalf("audit failure changed package: %+v %v", stored, err)
	}
	active, err := service.ActiveDescriptor(t.Context())
	if err != nil || !active.BuiltIn {
		t.Fatalf("audit failure changed activation: %+v %v", active, err)
	}
}
