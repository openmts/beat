package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
)

type fakeChannelOps struct {
	mu          sync.Mutex
	pending     []model.AgentTask
	completed   []model.AgentTaskResult
	enqueued    []string
	registerErr error
	completeErr error
}

func (f *fakeChannelOps) EnqueueAuthorizedKeyTask(_ context.Context, nodeID string, publicKey string) (*model.AgentTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueued = append(f.enqueued, nodeID+":"+publicKey)
	return &model.AgentTask{ID: "task-" + nodeID, NodeID: nodeID, Status: model.AgentTaskStatusPending}, nil
}

func (f *fakeChannelOps) RegisterAgent(_ context.Context, nodeID string) ([]model.AgentTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	return f.pending, nil
}

func (f *fakeChannelOps) CompleteAgentTask(_ context.Context, taskID string, status string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, model.AgentTaskResult{TaskID: taskID, Success: status == model.AgentTaskStatusDone})
	return f.completeErr
}

func agentChannelRequest(t *testing.T, nodeID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/ws", nil)
	ctx := middleware.WithAgentIdentity(req.Context(), model.AgentIdentity{NodeID: nodeID, NodeName: "agent-node"})
	return req.WithContext(ctx)
}

func TestAgentChannelHandlerUnauthorized(t *testing.T) {
	handler := NewAgentChannelHandler(&fakeChannelOps{}, service.NewAgentHub())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/ws", nil)
	recorder := httptest.NewRecorder()
	handler.HandleAgentWebSocket(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAgentChannelHandlerNilOperations(t *testing.T) {
	handler := NewAgentChannelHandler(nil, service.NewAgentHub())
	req := agentChannelRequest(t, "node-1")
	recorder := httptest.NewRecorder()
	handler.HandleAgentWebSocket(recorder, req)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestUnmarshalAuthorizedKeyTaskRequest(t *testing.T) {
	key, err := UnmarshalAuthorizedKeyTaskRequest([]byte(`{"public_key":"ssh-ed25519 aaa"}`))
	if err != nil || key != "ssh-ed25519 aaa" {
		t.Fatalf("key = %q, err = %v", key, err)
	}
	for _, raw := range []string{`{}`, `{"public_key":""}`, `not-json`} {
		if _, err := UnmarshalAuthorizedKeyTaskRequest([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid payload %q", raw)
		}
	}
}

var _ AgentChannelOperations = (*fakeChannelOps)(nil)

func TestAgentChannelWebSocketLifecycle(t *testing.T) {
	ops := &fakeChannelOps{
		pending: []model.AgentTask{
			{ID: "pending-1", NodeID: "node-1", Type: model.AgentTaskAuthorizedKey, Payload: `{"public_key":"ssh-ed25519 aaa"}`},
		},
	}
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(ops, hub)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/ws", func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithAgentIdentity(r.Context(), model.AgentIdentity{
			NodeID: r.Header.Get("X-Agent"), NodeName: "agent-node",
		})
		handler.HandleAgentWebSocket(w, r.WithContext(ctx))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	serverURL.Path = "/api/v1/agents/ws"
	serverURL.Scheme = "ws"

	conn, err := websocket.DialConfig(&websocket.Config{
		Location: serverURL,
		Origin:   serverURL,
		Header:   http.Header{"X-Agent": []string{"node-1"}},
		Version:  websocket.ProtocolVersionHybi13,
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Expect the pending task to be replayed on connect.
	var received struct {
		Type string          `json:"type"`
		Task model.AgentTask `json:"task"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if err := websocket.JSON.Receive(conn, &received); err != nil {
		t.Fatalf("receive pending task: %v", err)
	}
	if received.Type != "task" || received.Task.ID != "pending-1" {
		t.Fatalf("received = %#v", received)
	}

	// Send a task result back and verify it is recorded.
	if err := websocket.JSON.Send(conn, agentChannelMessage{
		Type:   agentTaskResultMessage,
		Result: &model.AgentTaskResult{TaskID: "pending-1", Success: true},
	}); err != nil {
		t.Fatalf("send result: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops.mu.Lock()
		done := len(ops.completed)
		ops.mu.Unlock()
		if done == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ops.mu.Lock()
	completed := len(ops.completed)
	ops.mu.Unlock()
	if completed != 1 {
		t.Fatalf("completed results = %d, want 1", completed)
	}
}

func TestAgentChannelHandlerWithAgentIdentity(t *testing.T) {
	// The handler must require an agent identity in the context.
	handler := NewAgentChannelHandler(&fakeChannelOps{}, service.NewAgentHub())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/ws", nil)
	recorder := httptest.NewRecorder()
	handler.HandleAgentWebSocket(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}
