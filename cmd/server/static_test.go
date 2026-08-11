package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPAHandler(t *testing.T) {
	staticDir := t.TempDir()
	writeStaticFile(t, staticDir, "index.html", "app-shell")
	writeStaticFile(t, staticDir, "assets/app.js", "application-code")
	handler := newSPAHandler(staticDir)

	tests := []struct {
		name       string
		method     string
		path       string
		accept     string
		wantStatus int
		wantBody   string
	}{
		{name: "root", path: "/", wantStatus: http.StatusOK, wantBody: "app-shell"},
		{name: "admin route", path: "/admin", wantStatus: http.StatusOK, wantBody: "app-shell"},
		{name: "node route", path: "/node/node-one", wantStatus: http.StatusOK, wantBody: "app-shell"},
		{name: "HTML route with extension", path: "/release/v1.2", accept: "text/html", wantStatus: http.StatusOK, wantBody: "app-shell"},
		{name: "static asset", path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "application-code"},
		{name: "missing asset", path: "/assets/missing.js", wantStatus: http.StatusNotFound, wantBody: "404 page not found\n"},
		{name: "post route", method: http.MethodPost, path: "/admin", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			request := httptest.NewRequest(method, tt.path, nil)
			request.Header.Set("Accept", tt.accept)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || (tt.wantBody != "" && response.Body.String() != tt.wantBody) {
				t.Fatalf("response = %d %q, want %d %q", response.Code, response.Body.String(), tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestSPARouterPreservesAPI(t *testing.T) {
	staticDir := t.TempDir()
	writeStaticFile(t, staticDir, "index.html", "app-shell")
	router := http.NewServeMux()
	router.HandleFunc("/api/v1/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("api-response"))
	})
	router.Handle("/", newSPAHandler(staticDir))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "api-response" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
}

func writeStaticFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create static directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write static file: %v", err)
	}
}
