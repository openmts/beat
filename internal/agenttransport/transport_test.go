package agenttransport

import (
	"net/http"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestNoProxyTransportIgnoresEnvironmentAndBoundsHeaders(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	transport := NoProxyTransport()
	if transport.Proxy != nil || transport.MaxResponseHeaderBytes != maximumResponseHeaderBytes {
		t.Fatalf("transport policy is unsafe: header limit %d", transport.MaxResponseHeaderBytes)
	}
}

func TestNoProxyTransportFallsBackWhenDefaultIsCustom(t *testing.T) {
	previous := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	t.Cleanup(func() { http.DefaultTransport = previous })
	transport := NoProxyTransport()
	if transport == nil || transport.Proxy != nil || transport.MaxResponseHeaderBytes != maximumResponseHeaderBytes {
		t.Fatalf("fallback transport = %#v", transport)
	}
}
