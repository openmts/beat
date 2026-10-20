package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/deploymentaccess"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func panicHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
}

func TestCORS(t *testing.T) {
	t.Run("sets CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
		w := httptest.NewRecorder()

		handler := CORS(okHandler())
		handler.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("expected Access-Control-Allow-Origin '*', got %q", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
			t.Errorf("expected Access-Control-Allow-Methods 'GET, OPTIONS', got %q", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
			t.Errorf("expected Access-Control-Allow-Headers 'Content-Type', got %q", got)
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("OPTIONS returns 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/nodes", nil)
		w := httptest.NewRecorder()

		handler := CORS(okHandler())
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", w.Code)
		}
	})

	t.Run("admin routes do not allow wildcard origins", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
		w := httptest.NewRecorder()
		CORS(okHandler()).ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("admin CORS origin = %q", got)
		}
	})
}

func TestContentTypeJSON(t *testing.T) {
	t.Run("sets Content-Type header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		handler := ContentTypeJSON(okHandler())
		handler.ServeHTTP(w, req)

		if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("expected Content-Type 'application/json; charset=utf-8', got %q", got)
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestLogging(t *testing.T) {
	t.Run("calls next handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		handler := Logging(okHandler())
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestLoggingSupportsWebSocketHijacking(t *testing.T) {
	echo := websocket.Handler(func(conn *websocket.Conn) {
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			return
		}
		_ = websocket.Message.Send(conn, message)
	})
	server := httptest.NewServer(Logging(echo))
	t.Cleanup(server.Close)

	conn, err := websocket.Dial("ws"+server.URL[4:], "", "http://localhost/")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := websocket.Message.Send(conn, "ping"); err != nil {
		t.Fatalf("send: %v", err)
	}
	var reply string
	if err := websocket.Message.Receive(conn, &reply); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if reply != "ping" {
		t.Fatalf("reply = %q, want ping", reply)
	}
}

func TestRecovery(t *testing.T) {
	t.Run("recovers from panic", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		handler := Recovery(panicHandler())
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", w.Code)
		}
	})

	t.Run("normal request passes through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		handler := Recovery(okHandler())
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestWebSecurity(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "https://example.com/api/v1/admin/users", nil)
	request.Header.Set("Origin", "https://example.com")
	if !SameOrigin(request) || !StateChanging(request.Method) {
		t.Fatal("same-origin state-changing request was rejected")
	}
	request.Header.Set("Origin", "https://attacker.example")
	if SameOrigin(request) {
		t.Fatal("cross-origin request was accepted")
	}
	response := httptest.NewRecorder()
	SecurityHeaders(okHandler()).ServeHTTP(response, request)
	if response.Header().Get("Strict-Transport-Security") == "" ||
		response.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func TestObserveStatus(t *testing.T) {
	var observed []int
	handler := ObserveStatus(okHandler(), func(status int) {
		observed = append(observed, status)
	})
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if len(observed) != 1 || observed[0] != http.StatusOK {
		t.Fatalf("observed = %v, want [200]", observed)
	}
}

func TestObserveStatusTracksExplicitStatus(t *testing.T) {
	var observed []int
	handler := ObserveStatus(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), func(status int) {
		observed = append(observed, status)
	})
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if len(observed) != 1 || observed[0] != http.StatusCreated {
		t.Fatalf("observed = %v, want [201]", observed)
	}
}

func TestResponseWriterLifecycle(t *testing.T) {
	recorder := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: recorder}

	if unwrapped := rw.Unwrap(); unwrapped != recorder {
		t.Fatalf("Unwrap returned the wrong writer")
	}
	if n, err := rw.Write([]byte("hello")); err != nil || n != 5 {
		t.Fatalf("write = %d, %v", n, err)
	}
	if rw.statusCode != http.StatusOK {
		t.Fatalf("implicit status = %d, want 200", rw.statusCode)
	}
	rw.WriteHeader(http.StatusInternalServerError)
	if rw.statusCode != http.StatusOK {
		t.Fatalf("explicit status overwrote implicit status: %d", rw.statusCode)
	}
	if got := recorder.Body.String(); got != "hello" {
		t.Fatalf("body = %q", got)
	}
}

func TestResponseWriterHijackUnsupported(t *testing.T) {
	recorder := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: recorder}
	if _, _, err := rw.Hijack(); err == nil {
		t.Fatal("expected hijack error for non-hijacker recorder")
	}
}

func TestLoggingWithObserverRecordsMetrics(t *testing.T) {
	type record struct {
		method   string
		route    string
		status   int
		duration time.Duration
	}
	var records []record
	observer := &recordingObserver{
		onObserve: func(method, route string, status int, duration time.Duration) {
			records = append(records, record{method, route, status, duration})
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey, "req-1"))
	handler := LoggingWithObserver(okHandler(), observer)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if len(records) != 1 {
		t.Fatalf("observed %d records, want 1", len(records))
	}
	got := records[0]
	if got.method != http.MethodGet || got.status != http.StatusOK {
		t.Fatalf("record = %+v", got)
	}
}

type recordingObserver struct {
	onObserve func(method, route string, status int, duration time.Duration)
}

func (o *recordingObserver) ObserveHTTP(method, route string, status int, duration time.Duration) {
	o.onObserve(method, route, status, duration)
}

func TestCORSWithDeploymentPolicy(t *testing.T) {
	policy, err := deploymentaccess.ParseOrigins("https://console.example.com,https://status.example.com:8443")
	if err != nil {
		t.Fatalf("parse origins: %v", err)
	}
	serve := func(t *testing.T, method, path, origin string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		CORSWithPolicy(policy)(okHandler()).ServeHTTP(recorder, request)
		return recorder
	}

	allowed := serve(t, http.MethodGet, "/api/v1/nodes", "https://console.example.com")
	if allowed.Header().Get("Access-Control-Allow-Origin") != "https://console.example.com" ||
		allowed.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("allowed headers = %v", allowed.Header())
	}
	if !slices.Contains(allowed.Header().Values("Vary"), "Origin") {
		t.Fatalf("allowed response must vary on origin: %v", allowed.Header())
	}
	if normalized := serve(t, http.MethodGet, "/api/v1/nodes", "HTTPS://Console.Example.com:443/").Header().Get("Access-Control-Allow-Origin"); normalized != "https://console.example.com" {
		t.Fatalf("origin was not normalized and echoed: %q", normalized)
	}
	if denied := serve(t, http.MethodGet, "/api/v1/nodes", "https://evil.example.com").Header().Get("Access-Control-Allow-Origin"); denied != "" {
		t.Fatalf("disallowed origin received %q", denied)
	}
	if admin := serve(t, http.MethodGet, "/api/v1/admin/users", "https://console.example.com").Header().Get("Access-Control-Allow-Origin"); admin != "" {
		t.Fatalf("admin routes must stay same-origin only, got %q", admin)
	}
	preflight := serve(t, http.MethodOptions, "/api/v1/nodes", "https://evil.example.com")
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("preflight = %d %v", preflight.Code, preflight.Header())
	}
	missing := serve(t, http.MethodGet, "/api/v1/nodes", "")
	if missing.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a request without Origin must not receive a wildcard header")
	}
	interaction := serve(t, http.MethodOptions, "/api/v1/public/interactions", "https://console.example.com")
	if interaction.Code != http.StatusNoContent || interaction.Header().Get("Access-Control-Allow-Methods") != "POST, OPTIONS" {
		t.Fatalf("interaction preflight = %d %v", interaction.Code, interaction.Header())
	}
	if interaction.Header().Get("Access-Control-Allow-Origin") != "https://console.example.com" {
		t.Fatalf("interaction preflight origin = %q", interaction.Header().Get("Access-Control-Allow-Origin"))
	}
}

// TestLoggingWithObserverNormalizesCanceledServerErrors 覆盖线上真实场景：浏览器
// 取消读取时处理器仍会写 500，但请求上下文已经结束、响应没有送达任何客户端。
// 这类请求必须记成客户端的 499，否则刷新页面与并发取消会把真实的 5xx 淹没。
func TestLoggingWithObserverNormalizesCanceledServerErrors(t *testing.T) {
	serverError := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	cases := []struct {
		name     string
		canceled bool
		expected int
	}{
		{name: "canceled request reports client closed", canceled: true, expected: statusClientClosedRequest},
		{name: "live request keeps server error", canceled: false, expected: http.StatusInternalServerError},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var recorded int
			observer := &recordingObserver{
				onObserve: func(_, _ string, status int, _ time.Duration) { recorded = status },
			}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
			if testCase.canceled {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			LoggingWithObserver(serverError, observer).ServeHTTP(httptest.NewRecorder(), request)
			if expected, actual := testCase.expected, recorded; expected != actual {
				t.Fatalf("expected observed status %d, actual %d", expected, actual)
			}
		})
	}
}
