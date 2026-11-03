package agent

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
)

func TestChannelReadLoopHonorsContextWhenResultQueueBlocked(t *testing.T) {
	taskSent := make(chan struct{})
	server := httptest.NewServer(websocket.Server{
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer close(taskSent)
			if err := websocket.JSON.Send(conn, channelMessage{
				Type: "task",
				Task: &model.AgentTask{ID: "task-1", NodeID: "node-1"},
			}); err != nil {
				t.Errorf("send task: %v", err)
			}
		}),
	})
	t.Cleanup(server.Close)

	channel, err := NewChannel(server.URL, "token", &recordingExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	out := make(chan outgoing, 1)
	out <- outgoing{payload: pingPongMessage{Type: "pong"}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writeDone := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- channel.readLoop(ctx, conn, out, writeDone) }()

	select {
	case <-taskSent:
	case <-time.After(time.Second):
		t.Fatal("server did not send task")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("read loop blocked on a full result queue after cancellation")
	}
}

func TestChannelReadLoopStopsWhenWriterExits(t *testing.T) {
	taskSent := make(chan struct{})
	server := httptest.NewServer(websocket.Server{
		Handler: websocket.Handler(func(conn *websocket.Conn) {
			defer close(taskSent)
			if err := websocket.JSON.Send(conn, channelMessage{
				Type: "task",
				Task: &model.AgentTask{ID: "task-1", NodeID: "node-1"},
			}); err != nil {
				t.Errorf("send task: %v", err)
			}
		}),
	})
	t.Cleanup(server.Close)

	channel, err := NewChannel(server.URL, "token", &recordingExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http"), "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	out := make(chan outgoing, 1)
	out <- outgoing{payload: pingPongMessage{Type: "pong"}}
	writeDone := make(chan struct{})
	close(writeDone)
	done := make(chan error, 1)
	go func() { done <- channel.readLoop(t.Context(), conn, out, writeDone) }()

	select {
	case <-taskSent:
	case <-time.After(time.Second):
		t.Fatal("server did not send task")
	}
	select {
	case err := <-done:
		if !errors.Is(err, errChannelWriterStopped) {
			t.Fatalf("read loop error = %v, want %v", err, errChannelWriterStopped)
		}
	case <-time.After(time.Second):
		t.Fatal("read loop blocked after its writer exited")
	}
}
