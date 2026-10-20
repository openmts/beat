package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONBodyLimitOnlyWrapsJSONRequests(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantLimited bool
	}{
		{name: "json", contentType: "application/json", wantLimited: true},
		{name: "json charset", contentType: "application/json; charset=utf-8", wantLimited: true},
		{name: "octet stream", contentType: "application/octet-stream"},
		{name: "zip", contentType: "application/zip"},
		{name: "missing", contentType: ""},
		{name: "invalid", contentType: "application/json; charset="},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var seen error
			handler := JSONBodyLimit(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, seen = io.ReadAll(r.Body)
			}))
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if test.wantLimited != (seen != nil) {
				t.Fatalf("read error = %v, want limited %v", seen, test.wantLimited)
			}
		})
	}
}
