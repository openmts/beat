package agentinstall

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentjoin"
)

func TestJoinRejectsServerErrorsWithoutLeakingSecrets(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", http.StatusUnauthorized, ""}, {"expired", http.StatusGone, ""},
		{"conflict", http.StatusConflict, ""}, {"server error", http.StatusInternalServerError, ""},
		{"invalid JSON", http.StatusOK, `not-json`}, {"zero code", http.StatusOK, `{"code":0,"message":"success","data":{}}`},
		{"failed envelope", http.StatusOK, `{"code":1,"message":"secret"}`},
		{"trailing JSON", http.StatusOK, `{} {}`}, {"unknown field", http.StatusOK, `{"secret":true}`},
		{"too large", http.StatusOK, strings.Repeat(" ", maxPrivateFile+1)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.respond = func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}
			_, err := f.installer.Join(context.Background(), f.options)
			if err == nil {
				t.Fatal("expected server response error")
			}
			if strings.Contains(err.Error(), f.options.Token) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("response leaked a secret: %v", err)
			}
		})
	}
}

func TestJoinRejectsRedirectWithoutForwardingCredentials(t *testing.T) {
	f := newFixture(t)
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	t.Cleanup(destination.Close)
	f.respond = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected redirect rejection")
	}
	if redirected {
		t.Fatal("join followed a redirect with credentials")
	}
}

func TestJoinTimeoutAndCancelPreservePendingRequest(t *testing.T) {
	f := newFixture(t)
	f.installer.client.Timeout = 10 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.respond = func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.installer.Join(ctx, f.options); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestJoinDoesNotDisableTLSVerification(t *testing.T) {
	f := newFixture(t)
	server := httptest.NewTLSServer(http.HandlerFunc(f.serveJoin))
	t.Cleanup(server.Close)
	f.options.ServerURL, f.result.Config.ServerURL = server.URL, server.URL
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected untrusted TLS certificate error")
	}
}

func TestJoinRejectsMismatchedResult(t *testing.T) {
	cases := map[string]func(*agentjoin.Result){
		"node":         func(r *agentjoin.Result) { r.NodeID = "" },
		"server":       func(r *agentjoin.Result) { r.Config.ServerURL = "https://attacker.example" },
		"credential":   func(r *agentjoin.Result) { r.Config.AgentToken = "replacement" },
		"name":         func(r *agentjoin.Result) { r.Config.NodeName = "\x00" },
		"port":         func(r *agentjoin.Result) { r.Config.SSHPort = 0 },
		"host":         func(r *agentjoin.Result) { r.Config.AdvertisedHost = "" },
		"invalid host": func(r *agentjoin.Result) { r.Config.AdvertisedHost = "bad/host" },
		"interval":     func(r *agentjoin.Result) { r.Config.ReportInterval = "1ms" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mutate(&f.result)
			if _, err := f.installer.Join(context.Background(), f.options); err == nil {
				t.Fatal("expected mismatched result error")
			}
		})
	}
}

func TestJoinHonorsExplicitNodeMetadata(t *testing.T) {
	f := newFixture(t)
	f.options.Name, f.options.Host, f.options.SSHPort = "chosen-node", "host.example.com", 2223
	f.result.Config.NodeName, f.result.Config.AdvertisedHost, f.result.Config.SSHPort = "chosen-node", "host.example.com", 2223
	if _, err := f.installer.Join(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	state, err := f.installer.enrollment(f.options)
	if err != nil {
		t.Fatal(err)
	}
	state.Result.Config.AdvertisedHost = "192.0.2.1"
	if err := validateResult(*state); err != nil {
		t.Fatalf("server metadata must remain authoritative on credential recovery: %v", err)
	}
}

func TestDecodeJSONErrors(t *testing.T) {
	var result agentjoin.Result
	if err := decodeJSON(errorReader{}, &result); err == nil {
		t.Fatal("expected read error")
	}
	if err := decodeJSON(strings.NewReader(`{} trailing`), &result); err == nil {
		t.Fatal("expected trailing value error")
	}
	if err := decodeJSON(strings.NewReader(`{"node_id":"id"}`), &result); err != nil {
		t.Fatal(err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestJoinResponseUsesBoundedDecoder(t *testing.T) {
	f := newFixture(t)
	f.respond = func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(agentjoin.Envelope[agentjoin.Result]{Code: http.StatusOK, Message: "success", Data: f.result})
		_, _ = io.WriteString(w, "\n{}")
	}
	if _, err := f.installer.Join(context.Background(), f.options); err == nil {
		t.Fatal("expected trailing response rejection")
	}
}
