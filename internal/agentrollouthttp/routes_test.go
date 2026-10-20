package agentrollouthttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtectedRegistrationAppliesEveryRouteClass(t *testing.T) {
	h := New(nil)
	mux := http.NewServeMux()
	guard := func(name string) func(http.Handler) http.Handler {
		return func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Guard", name); w.WriteHeader(204) })
		}
	}
	h.RegisterProtected(mux, Protection{OwnerRead: guard("read"), OwnerWrite: guard("write"), Agent: guard("agent")})
	for _, route := range h.routes() {
		method, path, _ := strings.Cut(route.pattern, " ")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		expected := map[access]string{ownerRead: "read", ownerWrite: "write", agentAccess: "agent"}[route.access]
		if response.Code != 204 || response.Header().Get("Guard") != expected {
			t.Fatal(route.pattern, response.Code, response.Header())
		}
	}
}

func TestProtectedRegistrationMissingGuardFailsClosed(t *testing.T) {
	h := New(nil)
	mux := http.NewServeMux()
	h.RegisterProtected(mux, Protection{})
	for _, route := range h.routes() {
		method, path, _ := strings.Cut(route.pattern, " ")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != 503 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(route.pattern, response.Code)
		}
	}
}
