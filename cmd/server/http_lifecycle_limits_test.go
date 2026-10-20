package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPServerPinsEntryLimits(t *testing.T) {
	server := newHTTPServer(http.NotFoundHandler())
	if server.MaxHeaderBytes != maximumHeaderBytes || maximumHeaderBytes != 32<<10 {
		t.Fatalf("header limit: %d", server.MaxHeaderBytes)
	}
	if server.ReadHeaderTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("timeouts: %+v", server)
	}
}

func TestRequestTargetHandlerRejectsOversizedTargets(t *testing.T) {
	target := "/api/v1/nodes/" + strings.Repeat("a", maximumRequestTargetBytes)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	newHTTPServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("oversized request reached the router")
	})).Handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestURITooLong {
		t.Fatalf("status: %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "request target is too long") {
		t.Fatalf("body: %s", recorder.Body.String())
	}
}

func TestRequestTargetHandlerPassesBoundedTargets(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.RequestURI = strings.Repeat("b", maximumRequestTargetBytes)
	reached := false
	newHTTPServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		reached = true
		writer.WriteHeader(http.StatusNoContent)
	})).Handler.ServeHTTP(recorder, request)
	if !reached || recorder.Code != http.StatusNoContent {
		t.Fatalf("bounded target rejected: %d", recorder.Code)
	}
}
