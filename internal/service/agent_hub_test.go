package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/beat/backend/internal/model"
)

type fakeAgentConn struct {
	mu       sync.Mutex
	closed   bool
	writes   []model.AgentTask
	writeErr error
}

func (f *fakeAgentConn) WriteTask(_ context.Context, task model.AgentTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, task)
	return nil
}

func (f *fakeAgentConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeAgentConn) written() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func TestAgentHubRegisterAndPush(t *testing.T) {
	hub := NewAgentHub()
	conn := &fakeAgentConn{}
	hub.Register("node-a", conn)

	if !hub.Online("node-a") {
		t.Fatal("node-a should be online")
	}
	task := model.AgentTask{ID: "t1", NodeID: "node-a", Type: model.AgentTaskAuthorizedKey}
	if !hub.PushTask(context.Background(), task) {
		t.Fatal("push to online node should succeed")
	}
	if conn.written() != 1 {
		t.Fatalf("writes = %d, want 1", conn.written())
	}
}

func TestAgentHubPushOffline(t *testing.T) {
	hub := NewAgentHub()
	task := model.AgentTask{ID: "t1", NodeID: "node-a"}
	if hub.PushTask(context.Background(), task) {
		t.Fatal("push to offline node should fail")
	}
}

func TestAgentHubUnregister(t *testing.T) {
	hub := NewAgentHub()
	conn := &fakeAgentConn{}
	hub.Register("node-a", conn)
	hub.Unregister("node-a", conn)
	if hub.Online("node-a") {
		t.Fatal("node-a should be offline after unregister")
	}
}

func TestAgentHubRegisterReplacesConnection(t *testing.T) {
	hub := NewAgentHub()
	first := &fakeAgentConn{}
	second := &fakeAgentConn{}
	hub.Register("node-a", first)
	hub.Register("node-a", second)
	if !first.closed {
		t.Fatal("replaced connection should be closed")
	}
	task := model.AgentTask{ID: "t1", NodeID: "node-a"}
	if !hub.PushTask(context.Background(), task) {
		t.Fatal("push should succeed via new connection")
	}
	if second.written() != 1 {
		t.Fatalf("new connection writes = %d, want 1", second.written())
	}
}

func TestAgentHubPushErrorClosesConnection(t *testing.T) {
	hub := NewAgentHub()
	conn := &fakeAgentConn{writeErr: errors.New("write failed")}
	hub.Register("node-a", conn)
	task := model.AgentTask{ID: "t1", NodeID: "node-a"}
	if hub.PushTask(context.Background(), task) {
		t.Fatal("push with write error should fail")
	}
	if !conn.closed {
		t.Fatal("connection should be closed after write error")
	}
	if hub.Online("node-a") {
		t.Fatal("node-a should be removed after write error")
	}
}

func TestNewTaskMessage(t *testing.T) {
	task := model.AgentTask{ID: "t1", NodeID: "node-a", Type: model.AgentTaskAuthorizedKey}
	raw, err := NewTaskMessage(task)
	if err != nil {
		t.Fatalf("new task message: %v", err)
	}
	var decoded struct {
		Type string          `json:"type"`
		Task model.AgentTask `json:"task"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if decoded.Type != "task" || decoded.Task.ID != "t1" {
		t.Fatalf("decoded = %#v", decoded)
	}
}
