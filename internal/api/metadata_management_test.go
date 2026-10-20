package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMetadataCredentialRoutesRequireSessionAndRecentAuth(t *testing.T) {
	router := setupSessionRouter(t)
	body := `{"name":"managed","host":"127.0.0.1","port":22,"server_url":"https://beat.example"}`
	legacy := requestRouter(router, http.MethodPost, "/api/v1/nodes", body, "Bearer admin-secret", "http://example.com")
	if legacy.Code != http.StatusForbidden {
		t.Fatal("bootstrap issued agent credential", legacy.Code)
	}
	cookie := bootstrapTerminalOwner(t, router)
	stale := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes", body, cookie, "http://example.com")
	if stale.Code != http.StatusPreconditionRequired {
		t.Fatal("stale session issued agent credential", stale.Code)
	}
	group := requestRouterWithCookie(router, http.MethodPost, "/api/v1/groups", `{"name":"ordinary"}`, cookie, "http://example.com")
	if group.Code != http.StatusCreated {
		t.Fatal("ordinary metadata unexpectedly required recent auth", group.Code, group.Body.String())
	}
	reauth := requestRouterWithCookie(router, http.MethodPost, "/api/v1/auth/reauthenticate", `{"password":"correct horse battery staple"}`, cookie, "http://example.com")
	if reauth.Code != http.StatusOK {
		t.Fatal(reauth.Code)
	}
	created := requestRouterWithCookie(router, http.MethodPost, "/api/v1/nodes", body, cookie, "http://example.com")
	if created.Code != http.StatusCreated || created.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(created.Code, created.Body.String())
	}
	var response struct {
		Data struct{ Node struct{ ID string } }
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	principal, err := router.security.Authenticate(t.Context(), cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	page, err := router.security.ListAuditEvents(t.Context(), &principal, model.AuditFilter{Action: "node.create", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	successes := 0
	for _, event := range page.Events {
		if event.Outcome == model.AuditOutcomeSuccess {
			successes++
			if event.ResourceID != response.Data.Node.ID || event.ResourceType != "node" {
				t.Fatal("wrong metadata audit target", event)
			}
		}
	}
	if successes != 1 {
		t.Fatal("duplicate or missing atomic audit", successes)
	}
}

func TestMetadataAuditPolicyInventory(t *testing.T) {
	cases := map[string]string{
		"POST /api/v1/nodes": "node.create", "PUT /api/v1/nodes/{id}": "node.update", "PUT /api/v1/nodes/sort": "node.sort",
		"POST /api/v1/nodes/{id}/token/rotate": "node.token.rotate", "POST /api/v1/nodes/{id}/token/revoke": "node.token.revoke",
		"POST /api/v1/groups": "group.create", "PUT /api/v1/groups/{id}": "group.update", "DELETE /api/v1/groups/{id}": "group.delete",
		"PUT /api/v1/groups/sort": "group.sort", "PUT /api/v1/groups/{id}/default": "group.default", "PUT /api/v1/settings/site": "site_settings.update",
		"POST /api/v1/ssh-keys": "ssh_key.import", "POST /api/v1/ssh-keys/generate": "ssh_key.generate", "DELETE /api/v1/ssh-keys/{id}": "ssh_key.delete", "GET /api/v1/ssh-keys/{id}": "ssh_key.reveal",
	}
	for pattern, expected := range cases {
		request := &http.Request{Pattern: pattern}
		request.SetPathValue("id", "resource")
		action, kind, _ := metadataAuditPolicy(request)
		if action != expected || kind == "" {
			t.Fatal(pattern, action, kind)
		}
	}
}
