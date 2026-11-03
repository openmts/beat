package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgentProtocolClientsDoNotInheritEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	reporter, err := NewHTTPReporter(Config{ServerURL: "https://beat.example", AgentToken: "token", NodeName: "node", AdvertisedHost: "192.0.2.1", SSHPort: 22, ReportInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	network, err := NewNetworkHTTPClient(Config{ServerURL: "https://beat.example", AgentToken: "token", NodeName: "node"})
	if err != nil {
		t.Fatal(err)
	}
	for name, client := range map[string]*http.Client{"reporter": reporter.client, "network": network.client} {
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil {
			t.Fatalf("%s client inherited an environment proxy", name)
		}
	}
}

func TestNetworkHTTPClientRejectsRedirectAndOversizedJSON(t *testing.T) {
	destinationHit := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()
	network, err := NewNetworkHTTPClient(Config{ServerURL: redirecting.URL, AgentToken: "token", NodeName: "node"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = network.FetchAssignments(t.Context()); err == nil || destinationHit {
		t.Fatalf("assignment redirect was followed: hit=%t err=%v", destinationHit, err)
	}

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"tasks":[]}}` + strings.Repeat(" ", maxNetworkResponseBody)))
	}))
	defer oversized.Close()
	network, err = NewNetworkHTTPClient(Config{ServerURL: oversized.URL, AgentToken: "token", NodeName: "node"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = network.FetchAssignments(t.Context()); err == nil {
		t.Fatal("oversized assignment response was accepted")
	}

	trailing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"tasks":[]}}{}`))
	}))
	defer trailing.Close()
	network, err = NewNetworkHTTPClient(Config{ServerURL: trailing.URL, AgentToken: "token", NodeName: "node"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = network.FetchAssignments(t.Context()); err == nil {
		t.Fatal("trailing assignment JSON was accepted")
	}
}
