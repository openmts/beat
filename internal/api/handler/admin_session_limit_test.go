package handler

import (
	"net/http"
	"strings"
	"testing"
)

func TestHandleListSessionsBoundsThePage(t *testing.T) {
	handler, principal, _ := newAuditPageHandler(t)
	for _, path := range []string{
		"/api/v1/admin/sessions?limit=0",
		"/api/v1/admin/sessions?limit=201",
		"/api/v1/admin/sessions?limit=abc",
	} {
		response := callAdminHandler(t, principal, http.MethodGet, path, "", handler.HandleListSessions)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", path, response.Code)
		}
	}
	accepted := callAdminHandler(t, principal, http.MethodGet, "/api/v1/admin/sessions?limit=5", "",
		handler.HandleListSessions)
	if accepted.Code != http.StatusOK {
		t.Fatalf("bounded sessions = %d, %s", accepted.Code, accepted.Body.String())
	}
	if !strings.Contains(accepted.Body.String(), `"current":true`) {
		t.Fatalf("expected the current-session marker, got %s", accepted.Body.String())
	}
}
