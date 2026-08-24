package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
)

func dialAgentChannel(t *testing.T, handler *AgentChannelHandler) *websocket.Conn {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/ws", func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithAgentIdentity(r.Context(), model.AgentIdentity{
			NodeID: r.Header.Get("X-Agent"), NodeName: "agent-node",
		})
		handler.HandleAgentWebSocket(w, r.WithContext(ctx))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

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
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// 单条结果落库失败不得断连：任务保持待执行、通道继续可用。
func TestAgentChannelResultFailureKeepsConnection(t *testing.T) {
	ops := &fakeChannelOps{completeErr: errFakeComplete}
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(ops, hub)
	conn := dialAgentChannel(t, handler)

	for i := range 2 {
		if err := websocket.JSON.Send(conn, agentChannelMessage{
			Type:   agentTaskResultMessage,
			Result: &model.AgentTaskResult{TaskID: "task-x", Success: true},
		}); err != nil {
			t.Fatalf("send result %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops.mu.Lock()
		completed := len(ops.completed)
		ops.mu.Unlock()
		if completed == 2 {
			return // 第二条结果也被处理，证明首条失败未断连
		}
		time.Sleep(10 * time.Millisecond)
	}
	ops.mu.Lock()
	defer ops.mu.Unlock()
	t.Fatalf("completed results = %d, want 2 (connection dropped early?)", len(ops.completed))
}

type stubError struct{ msg string }

func (e stubError) Error() string { return e.msg }

var errFakeComplete = stubError{msg: "store down"}

// pong 与未知帧不应中断结果流：pong 之后的结果仍被记录。
func TestAgentChannelIgnoresNonResultFrames(t *testing.T) {
	ops := &fakeChannelOps{}
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(ops, hub)
	conn := dialAgentChannel(t, handler)

	frames := []agentChannelMessage{
		{Type: "pong"},
		{Type: "unknown"},
		{Type: agentTaskResultMessage, Result: &model.AgentTaskResult{TaskID: "task-1", Success: false}},
	}
	for _, frame := range frames {
		if err := websocket.JSON.Send(conn, frame); err != nil {
			t.Fatalf("send %q frame: %v", frame.Type, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ops.mu.Lock()
		completed := len(ops.completed)
		ops.mu.Unlock()
		if completed == 1 {
			ops.mu.Lock()
			failed := !ops.completed[0].Success
			ops.mu.Unlock()
			if !failed {
				t.Fatal("expected failed status for the result")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("result after pong/unknown frames was not processed")
}
