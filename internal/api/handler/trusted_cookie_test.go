package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/api/middleware"
)

func TestAdminCookieIsSecureBehindTrustedProxy(t *testing.T) {
	trusted, err := middleware.ParseTrustedProxies("127.0.0.0/8")
	if err != nil {
		t.Fatalf("parse trusted proxies: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://internal.example/login", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	middleware.RequestContext(trusted)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		setAdminSessionCookie(w, request, "token", time.Now().Add(time.Hour))
	})).ServeHTTP(response, request)
	if !strings.Contains(response.Header().Get("Set-Cookie"), "; Secure;") {
		t.Fatalf("Set-Cookie = %q", response.Header().Get("Set-Cookie"))
	}
}
