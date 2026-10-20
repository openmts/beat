package beatcli

import (
	"net/http"
	"strings"
	"testing"
)

type runResult struct {
	stdout string
	stderr string
	err    error
}

func runCommand(t *testing.T, transport http.RoundTripper, getenv func(string) string, args ...string) runResult {
	t.Helper()
	var stdout, stderr strings.Builder
	options := Options{Output: &stdout, Errors: &stderr, Stdin: strings.NewReader(""), Getenv: getenv}
	if transport != nil {
		options.Transport = transport
	}
	err := NewCommand(options).Run(t.Context(), append([]string{"beat-cli"}, args...))
	return runResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func tokenArgs(t *testing.T) []string {
	t.Helper()
	return []string{"--" + tokenFlag, writeTokenFile(t, 0o600, "beat_service_v1_test")}
}

func serverArgs() []string { return []string{"--" + serverFlag, "http://127.0.0.1:8080"} }

func okBody() *http.Response {
	return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"status":"ok","version":"0.2.0"}}`)
}

func TestHealthCommandUnwrapsData(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	result := runCommand(t, transport, nil, append(append(serverArgs(), tokenArgs(t)...), "health")...)
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if !strings.Contains(result.stdout, `"status": "ok"`) {
		t.Fatalf("expected unwrapped status, got %q", result.stdout)
	}
	if got := transport.requests[0].URL.Path; got != runtimePath {
		t.Fatalf("expected %q, got %q", runtimePath, got)
	}
}

func TestHealthCommandHonoursRawAndRejectsArguments(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	args := append(append(serverArgs(), tokenArgs(t)...), "--raw", "health")
	result := runCommand(t, transport, nil, args...)
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if !strings.Contains(result.stdout, `"code": 200`) {
		t.Fatalf("expected the raw envelope, got %q", result.stdout)
	}
	extra := append(append(serverArgs(), tokenArgs(t)...), "health", "extra")
	if result := runCommand(t, transport, nil, extra...); result.err == nil || !strings.Contains(result.err.Error(), "positional") {
		t.Fatalf("expected a positional error, got %v", result.err)
	}
}

func TestRootCommandRequiresAKnownCommand(t *testing.T) {
	if result := runCommand(t, nil, nil); result.err == nil || !strings.Contains(result.err.Error(), "a command is required") {
		t.Fatalf("expected a usage error, got %v", result.err)
	}
	if result := runCommand(t, nil, nil, "bogus"); result.err == nil || !strings.Contains(result.err.Error(), "unknown command") {
		t.Fatalf("expected an unknown command error, got %v", result.err)
	}
}

func TestNodesCommandsBuildRequests(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) { return okBody(), nil }}
	base := append(serverArgs(), tokenArgs(t)...)
	list := runCommand(t, transport, nil, append(append([]string{}, base...), "nodes", "list")...)
	if list.err != nil {
		t.Fatalf("unexpected error: %v", list.err)
	}
	if got := transport.requests[0].URL.Path; got != nodesPath {
		t.Fatalf("expected %q, got %q", nodesPath, got)
	}
	metrics := runCommand(t, transport, nil, append(append([]string{}, base...),
		"nodes", "metrics", "node-1", "--metric", "cpu", "--from", "2026-10-14T00:00:00Z", "--to", "2026-10-14T01:00:00Z")...)
	if metrics.err != nil {
		t.Fatalf("unexpected error: %v", metrics.err)
	}
	query := transport.requests[1].URL.Query()
	if transport.requests[1].URL.Path != "/api/v1/nodes/node-1/metrics" || query.Get("metric") != "cpu" ||
		query.Get("from") != "2026-10-14T00:00:00Z" || query.Get("to") != "2026-10-14T01:00:00Z" {
		t.Fatalf("unexpected metrics request: %s", transport.requests[1].URL.String())
	}
	if result := runCommand(t, transport, nil, append(append([]string{}, base...), "nodes", "metrics")...); result.err == nil {
		t.Fatal("expected a missing node id error")
	}
	if result := runCommand(t, transport, nil, append(append([]string{}, base...), "nodes", "metrics", "a", "b")...); result.err == nil {
		t.Fatal("expected a node id count error")
	}
	if result := runCommand(t, transport, nil, append(append([]string{}, base...), "nodes", "list", "x")...); result.err == nil {
		t.Fatal("expected a positional error")
	}
}

func TestBackupsCommandsUseScopedEndpoints(t *testing.T) {
	transport := &stubTransport{handler: func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			return jsonResponse(http.StatusAccepted, `{"code":202,"message":"success","data":{"id":"b1","state":"running"}}`), nil
		}
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":[{"id":"b1"}]}`), nil
	}}
	base := append(serverArgs(), tokenArgs(t)...)
	if result := runCommand(t, transport, nil, append(append([]string{}, base...), "backups", "list")...); result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	created := runCommand(t, transport, nil, append(append([]string{}, base...), "backups", "create")...)
	if created.err != nil {
		t.Fatalf("unexpected error: %v", created.err)
	}
	if transport.requests[1].Method != http.MethodPost || transport.requests[1].URL.Path != backupsPath {
		t.Fatalf("unexpected create request: %s %s", transport.requests[1].Method, transport.requests[1].URL.Path)
	}
	if !strings.Contains(created.stdout, `"state": "running"`) {
		t.Fatalf("expected unwrapped create result, got %q", created.stdout)
	}
	if result := runCommand(t, transport, nil, append(append([]string{}, base...), "backups", "create", "x")...); result.err == nil {
		t.Fatal("expected a positional error")
	}
}

func TestAuditCommandBuildsFilters(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"events":[]}}`), nil
	}}
	base := append(serverArgs(), tokenArgs(t)...)
	plain := runCommand(t, transport, nil, append(append([]string{}, base...), "audit", "list")...)
	if plain.err != nil {
		t.Fatalf("unexpected error: %v", plain.err)
	}
	if raw := transport.requests[0].URL.RawQuery; raw != "" {
		t.Fatalf("expected no filters, got %q", raw)
	}
	filtered := runCommand(t, transport, nil, append(append([]string{}, base...),
		"audit", "list", "--action", "service_token.request", "--actor-id", "sp-1", "--limit", "25", "--offset", "50")...)
	if filtered.err != nil {
		t.Fatalf("unexpected error: %v", filtered.err)
	}
	query := transport.requests[1].URL.Query()
	if query.Get("action") != "service_token.request" || query.Get("actor_id") != "sp-1" ||
		query.Get("limit") != "25" || query.Get("offset") != "50" {
		t.Fatalf("unexpected audit query: %s", transport.requests[1].URL.RawQuery)
	}
	negative := append(append([]string{}, base...), "audit", "list", "--limit", "-1")
	if result := runCommand(t, transport, nil, negative...); result.err == nil || !strings.Contains(result.err.Error(), "must not be negative") {
		t.Fatalf("expected a paging validation error, got %v", result.err)
	}
}
