package agentdiagnostics

import (
	"net/http"
	"testing"
)

func TestDefaultTransportDoesNotInheritEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	client, err := NewClient(ClientConfig{ServerURL: "https://beat.example", Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("diagnostic client inherited an environment proxy")
	}
}
