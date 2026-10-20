package metricsfanout

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func upgradeRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://example.test/metrics", nil)
	r.Header.Set("Connection", "upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	return r
}

func TestHandshakeRejectionsReleaseReservation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"method", func(r *http.Request) { r.Method = http.MethodPost }, 400},
		{"connection", func(r *http.Request) { r.Header.Del("Connection") }, 400},
		{"version", func(r *http.Request) { r.Header.Set("Sec-WebSocket-Version", "12") }, 426},
		{"missing key", func(r *http.Request) { r.Header.Del("Sec-WebSocket-Key") }, 400},
		{"invalid key", func(r *http.Request) { r.Header.Set("Sec-WebSocket-Key", "invalid") }, 400},
		{"duplicate key", func(r *http.Request) { r.Header.Add("Sec-WebSocket-Key", "invalid") }, 400},
		{"oversize", func(r *http.Request) { r.Header.Set("Large", strings.Repeat("x", MaximumHeaderBytes)) }, 431},
		{"invalid IP", func(r *http.Request) { r.RemoteAddr = "invalid" }, 400},
		{"no hijacker", func(*http.Request) {}, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := fixture(t, nil)
			r := upgradeRequest()
			tc.mutate(r)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status || s.Stats().Reserved != 0 {
				t.Fatal(w.Code, s.Stats())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Header())
			}
		})
	}
}

func TestOriginRejection(t *testing.T) {
	s, _ := fixture(t, nil)
	s.config.AllowOrigin = func(*http.Request) bool { return false }
	w := httptest.NewRecorder()
	s.ServeHTTP(w, upgradeRequest())
	if w.Code != 403 || s.Stats().Reserved != 0 {
		t.Fatal(w.Code, s.Stats())
	}
}
