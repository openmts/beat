package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/service"
)

type fakeTerminalOperations struct {
	mu        sync.Mutex
	opened    string
	openError error
	results   []service.BatchResult
	nodeIDs   []string
	command   string
}

func (f *fakeTerminalOperations) OpenTerminal(
	_ context.Context,
	nodeID string,
	stream io.ReadWriteCloser,
) error {
	f.mu.Lock()
	f.opened = nodeID
	f.mu.Unlock()
	if f.openError != nil {
		return f.openError
	}
	_, err := stream.Write([]byte("ready"))
	return err
}

func (f *fakeTerminalOperations) ExecuteBatch(
	_ context.Context,
	nodeIDs []string,
	command string,
) []service.BatchResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodeIDs = append([]string(nil), nodeIDs...)
	f.command = command
	return f.results
}

func TestHandleTerminalWS(t *testing.T) {
	operations := &fakeTerminalOperations{}
	handler := NewTerminalHandler(operations)
	protected := middleware.WebSocketBearerAuth("admin-secret")(http.HandlerFunc(handler.HandleTerminalWS))
	server := httptest.NewServer(protected)
	t.Cleanup(server.Close)

	config, err := websocket.NewConfig("ws"+server.URL[4:]+"?node_id=node-one", server.URL)
	if err != nil {
		t.Fatalf("create websocket config: %v", err)
	}
	config.Protocol = []string{middleware.WebSocketTokenProtocol("admin-secret")}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("dial terminal: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	var message []byte
	if err := websocket.Message.Receive(conn, &message); err != nil {
		t.Fatalf("receive terminal: %v", err)
	}
	if string(message) != "ready" || operations.opened != "node-one" {
		t.Fatalf("message = %q, opened = %q", message, operations.opened)
	}
}

func TestHandleTerminalWSErrors(t *testing.T) {
	t.Run("missing node", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/ws/terminal", nil)
		response := httptest.NewRecorder()
		NewTerminalHandler(&fakeTerminalOperations{}).HandleTerminalWS(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})

	t.Run("service unavailable", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/ws/terminal?node_id=node", nil)
		response := httptest.NewRecorder()
		NewTerminalHandler(nil).HandleTerminalWS(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", response.Code)
		}
	})

	t.Run("service error is written", func(t *testing.T) {
		operations := &fakeTerminalOperations{openError: errors.New("connection refused")}
		handler := NewTerminalHandler(operations)
		protected := middleware.WebSocketBearerAuth("token")(http.HandlerFunc(handler.HandleTerminalWS))
		server := httptest.NewServer(protected)
		t.Cleanup(server.Close)
		config, err := websocket.NewConfig("ws"+server.URL[4:]+"?node_id=node", server.URL)
		if err != nil {
			t.Fatalf("config: %v", err)
		}
		config.Protocol = []string{middleware.WebSocketTokenProtocol("token")}
		conn, err := websocket.DialConfig(config)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			t.Fatalf("receive: %v", err)
		}
		_ = conn.Close()
		if !strings.Contains(message, "connection refused") {
			t.Fatalf("message = %q", message)
		}
	})
}

func TestHandleExecuteBatch(t *testing.T) {
	operations := &fakeTerminalOperations{results: []service.BatchResult{{NodeID: "node-one", Output: "ok"}}}
	handler := NewTerminalHandler(operations)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/terminal/execute", strings.NewReader(
		`{"node_ids":["node-one"],"command":"uptime"}`,
	))
	response := httptest.NewRecorder()

	handler.HandleExecuteBatch(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if operations.command != "uptime" || len(operations.nodeIDs) != 1 {
		t.Fatalf("command = %q, nodes = %v", operations.command, operations.nodeIDs)
	}
	var envelope Response
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestHandleExecuteBatchValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: "{"},
		{name: "missing nodes", body: `{"command":"uptime"}`},
		{name: "missing command", body: `{"node_ids":["node"]}`},
		{name: "command too long", body: `{"node_ids":["node"],"command":"` + strings.Repeat("x", 4097) + `"}`},
		{name: "too many nodes", body: `{"node_ids":["` + strings.Repeat(`node","`, 50) + `node"],"command":"uptime"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			NewTerminalHandler(&fakeTerminalOperations{}).HandleExecuteBatch(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", response.Code)
			}
		})
	}
}

func TestHandleExecuteBatchUnavailable(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"node_ids":["node"],"command":"uptime"}`,
	))
	response := httptest.NewRecorder()
	NewTerminalHandler(nil).HandleExecuteBatch(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func TestTerminalHandshake(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://example.test/ws", nil)
	if err := terminalHandshake(&websocket.Config{}, request); err == nil {
		t.Fatal("expected missing protocol error")
	}

	tests := []struct {
		name       string
		origin     string
		wantError  bool
		wantOrigin bool
	}{
		{name: "no origin"},
		{name: "invalid origin", origin: "://", wantError: true},
		{name: "cross origin", origin: "https://other.test", wantError: true},
		{name: "same origin", origin: "https://example.test", wantOrigin: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://example.test/ws", nil)
			request.Header.Set("Sec-WebSocket-Protocol", middleware.WebSocketTokenProtocol("token"))
			request.Header.Set("Origin", tt.origin)
			config := &websocket.Config{}
			var handshakeErr error
			next := http.HandlerFunc(func(_ http.ResponseWriter, authenticated *http.Request) {
				handshakeErr = terminalHandshake(config, authenticated)
			})
			response := httptest.NewRecorder()
			middleware.WebSocketBearerAuth("token")(next).ServeHTTP(response, request)
			err := handshakeErr
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, wantError = %v", err, tt.wantError)
			}
			if tt.wantOrigin && (config.Origin == nil || config.Origin.Host != "example.test") {
				t.Fatalf("origin = %#v", config.Origin)
			}
		})
	}
}
