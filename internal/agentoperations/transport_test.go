//go:build linux

package agentoperations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestAgentTransportIdentityDigestAndImmutableCompletion(t *testing.T) {
	lease := testLease("printf private-command")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer node-secret" || strings.Contains(request.URL.String(), "node-secret") {
			t.Error("node identity leaked or missing")
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/agent-operations/lease":
			var input protocol.AgentLeaseInput
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			digest, err := CapabilityDigest(input.Capabilities)
			if err != nil || input.CapabilityDigest != digest {
				t.Error("capability digest mismatch")
			}
			_ = json.NewEncoder(response).Encode(protocol.AgentResponse[*protocol.Lease]{Code: 200, Message: "success", Data: &lease})
		case "/api/v1/agent-operations/attempts/attempt-1/start":
			_ = json.NewEncoder(response).Encode(protocol.AgentResponse[protocol.StartAuthorization]{Code: 200, Data: protocol.StartAuthorization{Authorized: true}})
		case "/api/v1/agent-operations/attempts/attempt-1/heartbeat":
			_ = json.NewEncoder(response).Encode(protocol.AgentResponse[protocol.HeartbeatResponse]{Code: 200, Data: protocol.HeartbeatResponse{CancelRequested: true}})
		case "/api/v1/agent-operations/attempts/attempt-1/complete":
			var input protocol.AgentCompletionInput
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input.Nonce != lease.Nonce || string(input.Result.Stdout) != "output" {
				t.Error("completion lost content")
			}
			_ = json.NewEncoder(response).Encode(protocol.AgentResponse[protocol.CompletionReceipt]{Code: 200, Data: protocol.CompletionReceipt{Acknowledged: true}})
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{ServerURL: server.URL, Token: "node-secret"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := capabilities(normalizeConfig(testConfig(t)))
	if err != nil {
		t.Fatal(err)
	}
	returned, err := client.Lease(t.Context(), policy)
	if err != nil || returned.AttemptID != lease.AttemptID {
		t.Fatal("lease failed")
	}
	if err := client.Start(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	canceled, err := client.Heartbeat(t.Context(), lease)
	if err != nil || !canceled {
		t.Fatal("cancel response lost")
	}
	completion := protocol.Completion{Attempt: protocol.AttemptRequest{ID: lease.AttemptID, Nonce: lease.Nonce}, Result: protocol.Result{State: "succeeded", Stdout: []byte("output")}}
	if err := client.Complete(t.Context(), completion); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolFailuresDoNotReflectSensitiveResponses(t *testing.T) {
	for _, body := range []string{`{"code":200,"data":{"authorized":false}}`, `{"code":200,"unexpected":"secret"}`, `{"code":401,"message":"private command"}`, `not json`, strings.Repeat("x", maximumJournalBytes+1)} {
		t.Run("rejected", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(response, body) }))
			defer server.Close()
			client, err := NewClient(ClientConfig{ServerURL: server.URL, Token: "private-token"})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Start(t.Context(), testLease("true")); err == nil || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "private command") {
				t.Fatal("unsafe protocol error")
			}
		})
	}
	for _, address := range []string{"relative", "ftp://example.test", "https://user:secret@example.test", "https://example.test/?token=secret"} {
		if _, err := NewClient(ClientConfig{ServerURL: address, Token: "token"}); err == nil {
			t.Fatal("unsafe server URL accepted")
		}
	}
}

func TestProtocolNoLeaseAndNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, `{"code":200,"data":null}`)
	}))
	client, err := NewClient(ClientConfig{ServerURL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := capabilities(normalizeConfig(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := client.Lease(t.Context(), policy)
	if err != nil || lease != nil {
		t.Fatal("empty lease was not idle")
	}
	server.Close()
	if err := client.Start(context.Background(), testLease("true")); err == nil {
		t.Fatal("connection failure ignored")
	}
	leaseValue := testLease("true")
	leaseValue.AttemptID = "../escape"
	if err := client.Start(t.Context(), leaseValue); err == nil {
		t.Fatal("unsafe attempt URL accepted")
	}
	if _, err := CapabilityDigest(protocol.Policy{}); err == nil {
		t.Fatal("invalid capability digest accepted")
	}
}
