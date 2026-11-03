package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/egressproxy"
)

type egressProxyStub struct {
	settings egressproxy.Settings
	probe    egressproxy.ProbeResult
	err      error
	update   egressproxy.Update
}

func (stub *egressProxyStub) Get(context.Context) (egressproxy.Settings, error) {
	return stub.settings, stub.err
}

func (stub *egressProxyStub) Update(_ context.Context, input egressproxy.Update) (egressproxy.Settings, error) {
	stub.update = input
	return stub.settings, stub.err
}

func (stub *egressProxyStub) Probe(context.Context) (egressproxy.ProbeResult, error) {
	return stub.probe, stub.err
}

func TestEgressProxyHandlerContracts(t *testing.T) {
	service := &egressProxyStub{settings: egressproxy.Settings{Enabled: true, ProxyURL: "http://127.0.0.1:8080", Revision: 2}}
	handler := NewEgressProxyHandler(service)
	response := httptest.NewRecorder()
	handler.HandleGet(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"proxy_url":"http://127.0.0.1:8080"`) {
		t.Fatalf("get: %d %s", response.Code, response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"enabled":true,"proxy_url":"http://127.0.0.1:8080","username":"u","expected_revision":1}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.HandleUpdate(response, request)
	if response.Code != http.StatusOK || service.update.ExpectedRevision != 1 || service.update.Username != "u" {
		t.Fatalf("update: %d %+v %s", response.Code, service.update, response.Body.String())
	}
	service.probe = egressproxy.ProbeResult{LatencyMS: 23}
	response = httptest.NewRecorder()
	handler.HandleProbe(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"latency_ms":23`) {
		t.Fatalf("probe: %d %s", response.Code, response.Body.String())
	}
}

func TestEgressProxyHandlerErrors(t *testing.T) {
	for _, testCase := range []struct {
		err  error
		code int
	}{
		{egressproxy.ErrInvalid, http.StatusBadRequest},
		{egressproxy.ErrConflict, http.StatusConflict},
		{egressproxy.ErrDisabled, http.StatusConflict},
		{egressproxy.ErrProbeTimeout, http.StatusGatewayTimeout},
		{egressproxy.ErrProbeFailed, http.StatusBadGateway},
		{egressproxy.ErrUnavailable, http.StatusInternalServerError},
	} {
		handler := NewEgressProxyHandler(&egressProxyStub{err: testCase.err})
		response := httptest.NewRecorder()
		handler.HandleGet(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != testCase.code {
			t.Fatalf("%v returned %d", testCase.err, response.Code)
		}
		response = httptest.NewRecorder()
		handler.HandleProbe(response, httptest.NewRequest(http.MethodPost, "/", nil))
		if response.Code != testCase.code {
			t.Fatalf("probe %v returned %d", testCase.err, response.Code)
		}
	}
	handler := NewEgressProxyHandler(&egressProxyStub{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{`))
	request.Header.Set("Content-Type", "application/json")
	handler.HandleUpdate(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed body returned %d", response.Code)
	}
}
