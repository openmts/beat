package beatcli

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type stubTransport struct {
	handler  func(*http.Request) (*http.Response, error)
	requests []*http.Request
}

func (transport *stubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request)
	if transport.handler == nil {
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"status":"ok"}}`), nil
	}
	return transport.handler(request)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func redirectResponse(location string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusFound,
		Status:     http.StatusText(http.StatusFound),
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{"Location": []string{location}},
	}
}

func testBase(t *testing.T) *url.URL {
	t.Helper()
	base, err := ResolveServer("http://[::1]:8080")
	if err != nil {
		t.Fatalf("resolved server: %v", err)
	}
	return base
}

func testClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	base := testBase(t)
	return NewClient(base, "beat_service_v1_test", &http.Client{Transport: transport, CheckRedirect: sameHostRedirects(base.Host)})
}

func TestClientSendsBearerTokenAndQuery(t *testing.T) {
	transport := &stubTransport{}
	body, err := testClient(t, transport).Get(t.Context(), "/api/v1/nodes/manage", url.Values{"limit": {"5"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) == "" {
		t.Fatal("expected a response body")
	}
	if len(transport.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(transport.requests))
	}
	request := transport.requests[0]
	if got := request.URL.String(); got != "http://[::1]:8080/api/v1/nodes/manage?limit=5" {
		t.Fatalf("expected IPv6 URL with query, got %q", got)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer beat_service_v1_test" {
		t.Fatalf("expected bearer header, got %q", got)
	}
	if got := request.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("expected JSON accept header, got %q", got)
	}
}

func TestClientKeepsServerPathPrefix(t *testing.T) {
	base, err := ResolveServer("https://beat.example/beat")
	if err != nil {
		t.Fatalf("resolved server: %v", err)
	}
	transport := &stubTransport{}
	if _, err := NewClient(base, "beat_service_v1_test", &http.Client{Transport: transport}).Get(t.Context(), runtimePath, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := transport.requests[0].URL.Path; got != "/beat/api/v1/admin/runtime" {
		t.Fatalf("expected prefixed path, got %q", got)
	}
}

func TestClientClassifiesErrors(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		retryAfter   string
		want         string
		unauthorized bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"code":401,"message":"unauthorized"}`, want: "unauthorized (HTTP 401)", unauthorized: true},
		{name: "forbidden", status: http.StatusForbidden, body: `{"code":403,"message":"service token is not permitted"}`, want: "service token is not permitted (HTTP 403)", unauthorized: true},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"code":429,"message":"rate limit exceeded"}`, retryAfter: "60", want: "rate limit exceeded (HTTP 429, retry after 1m0s)"},
		{name: "plain text", status: http.StatusInternalServerError, body: "boom", want: "Internal Server Error (HTTP 500)"},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
				response := jsonResponse(scenario.status, scenario.body)
				if scenario.retryAfter != "" {
					response.Header.Set("Retry-After", scenario.retryAfter)
				}
				return response, nil
			}}
			_, err := testClient(t, transport).Get(t.Context(), runtimePath, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %v", err)
			}
			if apiErr.Error() != scenario.want {
				t.Fatalf("expected %q, got %q", scenario.want, apiErr.Error())
			}
			if apiErr.Status != scenario.status || apiErr.Unauthorized() != scenario.unauthorized {
				t.Fatalf("unexpected status mapping: %+v", apiErr)
			}
		})
	}
}

func TestClientRejectsOversizedResponses(t *testing.T) {
	payload := strings.Repeat("a", maximumResponseBytes+1)
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, payload), nil
	}}
	if _, err := testClient(t, transport).Get(t.Context(), runtimePath, nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected an oversize error, got %v", err)
	}
}

func TestClientReportsTransportFailure(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial refused")
	}}
	_, err := testClient(t, transport).Post(t.Context(), backupsPath)
	if err == nil || !strings.Contains(err.Error(), "POST "+backupsPath) || !strings.Contains(err.Error(), "dial refused") {
		t.Fatalf("expected a wrapped transport error, got %v", err)
	}
}

func TestClientDownloadStreamsBody(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("zip-bytes"))}, nil
	}}
	var sink strings.Builder
	written, err := testClient(t, transport).Download(t.Context(), backupsPath+"/b1/download", &sink)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != int64(len("zip-bytes")) || sink.String() != "zip-bytes" {
		t.Fatalf("unexpected download: written=%d body=%q", written, sink.String())
	}
}

func TestClientDownloadReportsWriteFailure(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("zip-bytes"))}, nil
	}}
	if _, err := testClient(t, transport).Download(t.Context(), backupsPath+"/b1/download", failingWriter{}); err == nil || !strings.Contains(err.Error(), "write download") {
		t.Fatalf("expected a write error, got %v", err)
	}
}

func TestClientRefusesCrossHostRedirects(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return redirectResponse("http://other.example/api/v1/admin/runtime"), nil
	}}
	_, err := testClient(t, transport).Get(t.Context(), runtimePath, nil)
	if err == nil || !strings.Contains(err.Error(), "refusing redirect to another host") {
		t.Fatalf("expected a refused redirect, got %v", err)
	}
}

func TestClientFollowsSameHostRedirects(t *testing.T) {
	attempts := 0
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return redirectResponse("http://[::1]:8080/api/v1/admin/runtime"), nil
		}
		return jsonResponse(http.StatusOK, `{"code":200,"message":"success","data":{"status":"ok"}}`), nil
	}}
	body, err := testClient(t, transport).Get(t.Context(), runtimePath, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("unexpected redirect handling: attempts=%d body=%s", attempts, body)
	}
}

func TestClientStopsRedirectLoops(t *testing.T) {
	transport := &stubTransport{handler: func(*http.Request) (*http.Response, error) {
		return redirectResponse("http://[::1]:8080/api/v1/admin/runtime"), nil
	}}
	_, err := testClient(t, transport).Get(t.Context(), runtimePath, nil)
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("expected a redirect loop error, got %v", err)
	}
}

func TestRetryAfterBoundsAndFormats(t *testing.T) {
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		value string
		want  time.Duration
	}{
		{value: "", want: 0},
		{value: "0", want: 0},
		{value: "-5", want: 0},
		{value: "60", want: time.Minute},
		{value: "999999999", want: maximumRetryAfter},
		{value: now.Add(30 * time.Second).Format(http.TimeFormat), want: 30 * time.Second},
		{value: now.Add(-time.Minute).Format(http.TimeFormat), want: 0},
		{value: "not-a-date", want: 0},
	}
	for _, scenario := range cases {
		if got := retryAfter(scenario.value, now); got != scenario.want {
			t.Fatalf("expected %s for %q, got %s", scenario.want, scenario.value, got)
		}
	}
}

func TestTruncateKeepsRunes(t *testing.T) {
	if got := truncate("  hello  ", 10); got != "hello" {
		t.Fatalf("expected trimmed value, got %q", got)
	}
	long := strings.Repeat("错", maximumErrorMessage+5)
	if got := truncate(long, maximumErrorMessage); len([]rune(got)) != maximumErrorMessage {
		t.Fatalf("expected rune bound, got %d", len([]rune(got)))
	}
}
