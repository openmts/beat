package agentdiagnostics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/beat/backend/internal/diagnosticprotocol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func wireResponse(status int, body string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: header,
		Body: io.NopCloser(strings.NewReader(body))}
}

func envelope(data string) string { return `{"code":200,"data":` + data + `}` }

func TestClientSpeaksPerNodeDiagnosticProtocol(t *testing.T) {
	var paths, bodies, authorizations []string
	client, err := NewClient(ClientConfig{ServerURL: "https://beat.example.com/", Token: "token",
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			content, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				return nil, readErr
			}
			paths = append(paths, request.URL.Path)
			bodies = append(bodies, string(content))
			authorizations = append(authorizations, request.Header.Get("Authorization"))
			switch {
			case strings.HasSuffix(request.URL.Path, "/capability"):
				return wireResponse(http.StatusOK, envelope(`{"digest":"`+strings.Repeat("a", 64)+`"}`)), nil
			case strings.HasSuffix(request.URL.Path, "/lease"):
				return wireResponse(http.StatusOK, envelope(`{"job_id":"job-1","attempt":2,"nonce":"nonce","expires_at":1,`+
					`"payload":{"kind":"mtr","destination":"8.8.8.8","probes":10,"hop_limit":32}}`)), nil
			case strings.HasSuffix(request.URL.Path, "/heartbeat"):
				return wireResponse(http.StatusOK, envelope(`{"cancel_requested":true}`)), nil
			case strings.HasSuffix(request.URL.Path, "/progress"):
				return wireResponse(http.StatusOK, envelope(`{"sequence":7}`)), nil
			case strings.HasSuffix(request.URL.Path, "/start"):
				return wireResponse(http.StatusOK, envelope(`{"acknowledged":true}`)), nil
			default:
				return wireResponse(http.StatusOK, envelope(`{"acknowledged":true}`)), nil
			}
		})})
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}
	ctx := context.Background()
	digest, err := client.Advertise(ctx, validConfig(t).Capability(capabilityStateReady))
	if err != nil || digest != strings.Repeat("a", 64) {
		t.Fatalf("Advertise = %q (%v)", digest, err)
	}
	lease, err := client.Lease(ctx, digest)
	if err != nil || lease == nil || lease.Attempt != 2 || lease.Payload.Kind != "mtr" || lease.Payload.Probes != 10 {
		t.Fatalf("Lease = %+v (%v)", lease, err)
	}
	attempt := diagnosticprotocol.AttemptRequest{JobID: "job-1", Nonce: "nonce", Attempt: 2}
	if err := client.Start(ctx, attempt); err != nil {
		t.Fatalf("Start error = %v", err)
	}
	cancel, err := client.Heartbeat(ctx, attempt)
	if err != nil || !cancel {
		t.Fatalf("Heartbeat = %v (%v)", cancel, err)
	}
	sequence, err := client.Progress(ctx, diagnosticprotocol.ProgressRequest{JobID: "job-1", Nonce: "nonce", Attempt: 2, Sequence: 7})
	if err != nil || sequence != 7 {
		t.Fatalf("Progress = %d (%v)", sequence, err)
	}
	if err := client.Complete(ctx, diagnosticprotocol.CompletionRequest{JobID: "job-1", Nonce: "nonce", Attempt: 2, State: "succeeded"}); err != nil {
		t.Fatalf("Complete error = %v", err)
	}
	want := []string{"/api/v1/agent-diagnostics/capability", "/api/v1/agent-diagnostics/lease",
		"/api/v1/agent-diagnostics/attempts/job-1/start", "/api/v1/agent-diagnostics/attempts/job-1/heartbeat",
		"/api/v1/agent-diagnostics/attempts/job-1/progress", "/api/v1/agent-diagnostics/attempts/job-1/complete"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %+v", paths)
	}
	for index, path := range want {
		if paths[index] != path {
			t.Errorf("path %d = %q, want %q", index, paths[index], path)
		}
		if authorizations[index] != "Bearer token" {
			t.Errorf("authorization %d = %q", index, authorizations[index])
		}
	}
	if !strings.Contains(bodies[3], `"job_id":"job-1"`) {
		t.Fatalf("heartbeat body = %q", bodies[3])
	}
}

func TestClientMapsRejectionsAndTransportFailures(t *testing.T) {
	respond := func(response *http.Response, err error) *Client {
		t.Helper()
		client, clientErr := NewClient(ClientConfig{ServerURL: "https://beat.example.com", Token: "token",
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response, err })})
		if clientErr != nil {
			t.Fatalf("NewClient error = %v", clientErr)
		}
		return client
	}
	attempt := diagnosticprotocol.AttemptRequest{JobID: "job-1", Nonce: "nonce", Attempt: 1}
	ctx := context.Background()
	lease, err := respond(wireResponse(http.StatusForbidden, `{"code":403,"message":"no"}`), nil).Lease(ctx, "digest")
	if lease != nil || err != nil {
		t.Fatalf("forbidden lease = %+v (%v)", lease, err)
	}
	if _, err := respond(wireResponse(http.StatusInternalServerError, ""), nil).Progress(ctx,
		diagnosticprotocol.ProgressRequest{JobID: "job-1", Sequence: 1}); !errors.Is(err, ErrTransport) {
		t.Fatalf("progress error = %v", err)
	}
	if err := respond(wireResponse(http.StatusOK, envelope(`{"acknowledged":false}`)), nil).Start(ctx, attempt); !errors.Is(err, ErrTransport) {
		t.Fatalf("start error = %v", err)
	}
	if err := respond(wireResponse(http.StatusOK, envelope(`{"acknowledged":false}`)), nil).Complete(ctx,
		diagnosticprotocol.CompletionRequest{JobID: "job-1"}); !errors.Is(err, ErrTransport) {
		t.Fatalf("complete error = %v", err)
	}
	if err := respond(wireResponse(http.StatusOK, `{"code":500,"data":null}`), nil).Start(ctx, attempt); !errors.Is(err, ErrTransport) {
		t.Fatalf("envelope error = %v", err)
	}
	encoded := wireResponse(http.StatusOK, envelope(`{"acknowledged":true}`))
	encoded.Header.Set("Content-Encoding", "gzip")
	if err := respond(encoded, nil).Start(ctx, attempt); !errors.Is(err, ErrTransport) {
		t.Fatalf("encoded error = %v", err)
	}
	if err := respond(wireResponse(http.StatusFound, ""), nil).Start(ctx, attempt); !errors.Is(err, ErrTransport) {
		t.Fatalf("redirect error = %v", err)
	}
	if _, err := respond(wireResponse(http.StatusOK, strings.Repeat("x", maximumResponseBytes+1)), nil).Lease(ctx, "digest"); !errors.Is(err, ErrTransport) {
		t.Fatalf("oversized error = %v", err)
	}
	if err := respond(nil, errors.New("dial failed")).Start(ctx, attempt); err == nil {
		t.Fatal("transport failure must surface")
	}
}

func TestNewClientRejectsUnsafeConfiguration(t *testing.T) {
	cases := []ClientConfig{
		{ServerURL: "https://beat.example.com", Token: ""},
		{ServerURL: "beat.example.com", Token: "token"},
		{ServerURL: "ws://beat.example.com", Token: "token"},
		{ServerURL: "https://user@beat.example.com", Token: "token"},
		{ServerURL: "https://beat.example.com?token=1", Token: "token"},
		{ServerURL: "https://beat.example.com#fragment", Token: "token"},
	}
	for _, config := range cases {
		if _, err := NewClient(config); !errors.Is(err, ErrTransport) {
			t.Fatalf("NewClient(%q) error = %v", config.ServerURL, err)
		}
	}
}
