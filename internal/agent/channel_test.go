package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
)

type recordingExecutor struct {
	mu      sync.Mutex
	results []model.AgentTaskResult
}

func (r *recordingExecutor) ExecuteTask(_ context.Context, task model.AgentTask) model.AgentTaskResult {
	result := model.AgentTaskResult{TaskID: task.ID, Success: true}
	r.mu.Lock()
	r.results = append(r.results, result)
	r.mu.Unlock()
	return result
}

func TestChannelReceivesTaskAndReportsResult(t *testing.T) {
	received := make(chan struct{})
	server := httptest.NewServer(websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			if r.Header.Get("Authorization") != "Bearer agent-token" {
				t.Error("expected agent token header")
			}
			return nil
		},
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer close(received)
			if err := websocket.JSON.Send(conn, channelMessage{
				Type: "task",
				Task: &model.AgentTask{ID: "task-1", NodeID: "node-1", Type: model.AgentTaskAuthorizedKey, Payload: `{"public_key":"ssh-ed25519 aaa"}`},
			}); err != nil {
				t.Errorf("send task: %v", err)
				return
			}
			var result taskResultMessage
			if err := websocket.JSON.Receive(conn, &result); err != nil {
				t.Errorf("receive result: %v", err)
				return
			}
			if result.Type != "task_result" || result.Result.TaskID != "task-1" || !result.Result.Success {
				t.Errorf("result = %#v", result)
			}
		}),
	})
	defer server.Close()

	executor := &recordingExecutor{}
	channel, err := NewChannel(server.URL, "agent-token", executor)
	if err != nil {
		t.Fatalf("new channel: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- channel.Run(ctx) }()

	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not complete handshake")
	}
	cancel()
	select {
	case <-runErr:
	case <-time.After(time.Second):
		t.Fatal("channel did not stop")
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if len(executor.results) != 1 {
		t.Fatalf("executor results = %d, want 1", len(executor.results))
	}
}

func TestChannelReconnects(t *testing.T) {
	var mu sync.Mutex
	connections := 0
	server := httptest.NewServer(websocket.Server{
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			mu.Lock()
			connections++
			count := connections
			mu.Unlock()
			if count == 1 {
				_ = conn.Close()
				return
			}
			var message channelMessage
			for {
				if err := websocket.JSON.Receive(conn, &message); err != nil {
					return
				}
			}
		}),
	})
	defer server.Close()

	channel, err := NewChannel(server.URL, "agent-token", &recordingExecutor{})
	if err != nil {
		t.Fatalf("new channel: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- channel.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		current := connections
		mu.Unlock()
		if current >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	final := connections
	mu.Unlock()
	if final < 2 {
		t.Fatalf("connections = %d, want at least 2 (reconnect)", final)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("channel did not stop")
	}
}

func TestNewChannelRejectsBadURL(t *testing.T) {
	for _, bad := range []string{"", "://bad", "ftp://host"} {
		if _, err := NewChannel(bad, "token", &recordingExecutor{}); err == nil {
			t.Fatalf("accepted invalid URL %q", bad)
		}
	}
}

func TestChannelReceivesPingAndKeepsListening(t *testing.T) {
	server := httptest.NewServer(websocket.Server{
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			// Send a keepalive ping first, then a task.
			if err := websocket.JSON.Send(conn, map[string]string{"type": "ping"}); err != nil {
				t.Errorf("send ping: %v", err)
				return
			}
			if err := websocket.JSON.Send(conn, channelMessage{
				Type: "task",
				Task: &model.AgentTask{ID: "task-1", NodeID: "node-1", Type: model.AgentTaskAuthorizedKey, Payload: `{"public_key":"ssh-ed25519 aaa"}`},
			}); err != nil {
				t.Errorf("send task: %v", err)
				return
			}
			var result taskResultMessage
			if err := websocket.JSON.Receive(conn, &result); err != nil {
				t.Errorf("receive result: %v", err)
			}
		}),
	})
	defer server.Close()

	executor := &recordingExecutor{}
	channel, err := NewChannel(server.URL, "agent-token", executor)
	if err != nil {
		t.Fatalf("new channel: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- channel.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		executor.mu.Lock()
		results := len(executor.results)
		executor.mu.Unlock()
		if results == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	executor.mu.Lock()
	results := len(executor.results)
	executor.mu.Unlock()
	if results != 1 {
		t.Fatalf("executor results = %d, want 1 (ping must not break the loop)", results)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("channel did not stop")
	}
}

func TestChannelEndpointConversion(t *testing.T) {
	channel, err := NewChannel("https://beat.example.com/base", "token", &recordingExecutor{})
	if err != nil {
		t.Fatalf("new channel: %v", err)
	}
	if got := channel.location.String(); !strings.HasPrefix(got, "wss://beat.example.com/base/api/v1/agents/ws") {
		t.Fatalf("location = %q", got)
	}
}
