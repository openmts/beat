package agentinstall

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDefaultHTTPClientDoesNotInheritEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	transport, ok := newHTTPClient().Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("installer client inherited an environment proxy")
	}
}

func TestDecodeGrantEnvelopeRejectsOversizedAndTrailingJSON(t *testing.T) {
	type payload struct {
		Value int `json:"value"`
	}
	valid := `{"code":200,"data":{"value":1}}`
	cases := map[string]string{
		"oversized": valid + strings.Repeat(" ", grantResponseLimit),
		"trailing":  valid + `{}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			if _, err := decodeGrantEnvelope[payload](response); err == nil {
				t.Fatal("invalid enrollment envelope was accepted")
			}
		})
	}
}
