package agent

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
)

// 收到服务端 ping 必须回复 pong，且任务执行不受影响（读写分离验证）。
func TestChannelRepliesToServerPingWithPong(t *testing.T) {
	gotPong := make(chan struct{})
	taskReceived := make(chan struct{})

	server := httptest.NewServer(websocket.Server{
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer close(taskReceived)
			if err := websocket.JSON.Send(conn, channelMessage{Type: "ping"}); err != nil {
				t.Errorf("send ping: %v", err)
				return
			}
			var reply channelMessage
			if err := websocket.JSON.Receive(conn, &reply); err != nil {
				t.Errorf("receive pong: %v", err)
				return
			}
			if reply.Type != "pong" {
				t.Errorf("reply type = %q, want pong", reply.Type)
				return
			}
			close(gotPong)

			if err := websocket.JSON.Send(conn, channelMessage{
				Type: "task",
				Task: &model.AgentTask{ID: "task-1", NodeID: "node-1", Type: model.AgentTaskAuthorizedKey},
			}); err != nil {
				t.Errorf("send task after pong: %v", err)
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
	case <-gotPong:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive pong for its ping")
	}
	select {
	case <-taskReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("task flow did not complete")
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
