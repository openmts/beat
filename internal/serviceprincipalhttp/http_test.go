package serviceprincipalhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/serviceprincipal"
	"github.com/beat/backend/internal/store"
)

type fixture struct {
	handler *Handler
	mux     *http.ServeMux
	store   *store.ServicePrincipalStore
	db      *store.SQLiteStore
}

func setupFixture(t *testing.T) *fixture {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	principals := store.NewServicePrincipalStore(database.DB)
	handler := New(serviceprincipal.NewService(principals))
	mux := http.NewServeMux()
	handler.RegisterProtected(mux, Protection{OwnerRead: ownerGuard, OwnerWrite: ownerGuard})
	return &fixture{handler: handler, mux: mux, store: principals, db: database}
}

func ownerGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := model.AdminPrincipal{
			User:    model.AdminUser{ID: "owner-one", Username: "owner", Role: model.AdminRoleOwner},
			Session: model.AdminSession{ID: "session-abcdef123456"},
		}
		next.ServeHTTP(w, r.WithContext(middleware.WithAdminPrincipal(r.Context(), principal)))
	})
}

func (f *fixture) call(t *testing.T, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	middleware.RequestContext(middleware.TrustedProxies{})(f.mux).ServeHTTP(recorder, request)
	payload := map[string]any{"data": map[string]any{}}
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
		}
	}
	return recorder, payload
}

func createBody() string {
	expiry := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	return `{"name":"nightly backup","purpose":"automation","scopes":["backups:create","nodes:read"],"resources":["node:node-one"],"visibility":"private","source_cidrs":["192.0.2.0/24"],"expires_at":"` + expiry + `"}`
}

func TestServiceTokenLifecycleThroughHTTPSurface(t *testing.T) {
	f := setupFixture(t)
	recorder, payload := f.call(t, http.MethodPost, "/api/v1/admin/service-tokens", createBody())
	if recorder.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", recorder.Code, recorder.Body.String())
	}
	data := payload["data"].(map[string]any)
	token, _ := data["token"].(string)
	principal := data["principal"].(map[string]any)
	if !strings.HasPrefix(token, model.ServiceTokenPrefix) {
		t.Fatal("creation did not return a one-time plaintext token", token)
	}
	if principal["name"] != "nightly backup" || principal["token_hash"] != nil {
		t.Fatal("creation exposed unexpected principal fields", principal)
	}
	id, _ := principal["id"].(string)

	recorder, payload = f.call(t, http.MethodGet, "/api/v1/admin/service-tokens", "")
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "token_hash") {
		t.Fatalf("list leaked a digest: %d %s", recorder.Code, recorder.Body.String())
	}
	items := payload["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatal("list did not return the created principal", items)
	}

	rotateBody := `{"name":"nightly backup v2","scopes":["backups:create"],"expires_at":"` +
		time.Now().UTC().Add(72*time.Hour).Format(time.RFC3339) + `","overlap_seconds":3600}`
	recorder, payload = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens/"+id+"/rotate", rotateBody)
	if recorder.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d %s", recorder.Code, recorder.Body.String())
	}
	successor := payload["data"].(map[string]any)["principal"].(map[string]any)
	if successor["rotated_from"] != id || successor["rotation_overlap_seconds"].(float64) != 3600 {
		t.Fatal("rotation did not record its predecessor", successor)
	}

	recorder, _ = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens/"+id+"/revoke", `{"reason":"replaced"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("revoke failed: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder, _ = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens/"+id+"/revoke", `{"reason":"again"}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("second revoke was not refused: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder, _ = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens/missing/revoke", `{"reason":"x"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown principal was not a 404: %d", recorder.Code)
	}
}

func TestIssuedTokenAuthenticatesThroughTheStore(t *testing.T) {
	f := setupFixture(t)
	_, payload := f.call(t, http.MethodPost, "/api/v1/admin/service-tokens", createBody())
	data := payload["data"].(map[string]any)
	token, _ := data["token"].(string)
	authenticator := serviceprincipal.NewAuthenticator(f.store)
	principal, err := authenticator.Authenticate(t.Context(), token, mustAddr(t, "192.0.2.10"))
	if err != nil {
		t.Fatal(err)
	}
	if principal.Name != "nightly backup" || len(principal.Scopes) != 2 {
		t.Fatal("issued token did not carry its policy", principal)
	}
	if _, err := authenticator.Authenticate(t.Context(), token, mustAddr(t, "198.51.100.7")); err == nil {
		t.Fatal("source restriction was not enforced end to end")
	}
}

func TestServiceTokenSurfaceRejectsInvalidRequests(t *testing.T) {
	f := setupFixture(t)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"unknown field", http.MethodPost, "/api/v1/admin/service-tokens", `{"name":"x","unknown":1}`, http.StatusBadRequest},
		{"unknown scope", http.MethodPost, "/api/v1/admin/service-tokens", `{"name":"x","scopes":["nodes:delete"],"expires_at":"` + time.Now().UTC().Add(time.Hour).Format(time.RFC3339) + `"}`, http.StatusBadRequest},
		{"empty scopes", http.MethodPost, "/api/v1/admin/service-tokens", `{"name":"x","expires_at":"` + time.Now().UTC().Add(time.Hour).Format(time.RFC3339) + `"}`, http.StatusBadRequest},
		{"negative overlap", http.MethodPost, "/api/v1/admin/service-tokens/principal/rotate", `{"name":"x","scopes":["health:read"],"expires_at":"` + time.Now().UTC().Add(time.Hour).Format(time.RFC3339) + `","overlap_seconds":-1}`, http.StatusBadRequest},
	}
	for _, test := range cases {
		recorder, _ := f.call(t, test.method, test.path, test.body)
		if recorder.Code != test.status {
			t.Fatalf("%s returned %d (%s)", test.name, recorder.Code, recorder.Body.String())
		}
	}
	anonymous := http.NewServeMux()
	passthrough := func(next http.Handler) http.Handler { return next }
	f.handler.RegisterProtected(anonymous, Protection{OwnerRead: passthrough, OwnerWrite: passthrough})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-tokens", nil)
	recorder := httptest.NewRecorder()
	middleware.RequestContext(middleware.TrustedProxies{})(anonymous).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("missing administrator identity returned %d", recorder.Code)
	}
}

func TestProtectionRefusesUnwiredRoutes(t *testing.T) {
	f := setupFixture(t)
	mux := http.NewServeMux()
	f.handler.RegisterProtected(mux, Protection{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-tokens", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unprotected route returned %d", recorder.Code)
	}
}

func mustAddr(t *testing.T, value string) netip.Addr {
	t.Helper()
	parsed, err := netip.ParseAddr(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestServiceTokenSurfaceRejectsMalformedTransport(t *testing.T) {
	f := setupFixture(t)
	expiry := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	recorder, _ := f.call(t, http.MethodPost, "/api/v1/admin/service-tokens",
		`{"name":"x","scopes":["health:read"],"resources":["node-one"],"expires_at":"`+expiry+`"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("resource without kind was accepted: %d %s", recorder.Code, recorder.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-tokens", strings.NewReader(createBody()))
	request.Header.Set("Content-Encoding", "gzip")
	wrapped := httptest.NewRecorder()
	middleware.RequestContext(middleware.TrustedProxies{})(f.mux).ServeHTTP(wrapped, request)
	if wrapped.Code != http.StatusBadRequest {
		t.Fatalf("compressed body was accepted: %d", wrapped.Code)
	}
	oversized := `{"name":"x","scopes":["health:read"],"purpose":"` + strings.Repeat("a", MaximumJSONBytes) + `"}`
	recorder, _ = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens", oversized)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized body was accepted: %d", recorder.Code)
	}
	recorder, _ = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens/missing/rotate",
		`{"name":"x","scopes":["health:read"],"expires_at":"`+expiry+`"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("rotating a missing principal returned %d", recorder.Code)
	}
	recorder, _ = f.call(t, http.MethodPost, "/api/v1/admin/service-tokens/missing/revoke",
		`{"reason":"`+strings.Repeat("a", model.MaximumServiceTokenPurpose+1)+`"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized revocation reason returned %d", recorder.Code)
	}
}
