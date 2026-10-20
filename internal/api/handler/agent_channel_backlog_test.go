package handler

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
)

func TestAgentChannelReplaysMoreThanSixteenTasksAndAcceptsResults(t *testing.T) {
	tasks := make([]model.AgentTask, 64)
	for index := range tasks {
		tasks[index] = model.AgentTask{ID: fmt.Sprintf("queued-%d", index), NodeID: "node-1", Type: model.AgentTaskAuthorizedKey, Payload: `{"public_key":"ssh-ed25519 test"}`}
	}
	ops := &fakeChannelOps{pending: tasks}
	hub := service.NewAgentHub()
	connection := dialAgentChannel(t, NewAgentChannelHandler(ops, hub))
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for index := range tasks {
		var message service.TaskMessage
		if err := websocket.JSON.Receive(connection, &message); err != nil {
			t.Fatalf("replay stopped at %d: %v", index, err)
		}
		if message.Task.ID != tasks[index].ID {
			t.Fatal("replay order changed")
		}
		if err := websocket.JSON.Send(connection, agentChannelMessage{Type: agentTaskResultMessage, Result: &model.AgentTaskResult{TaskID: message.Task.ID, Success: true}}); err != nil {
			t.Fatal(err)
		}
	}
	receiveAgentRefresh(t, connection)
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	awaitAgentOffline(t, hub)
	ops.mu.Lock()
	defer ops.mu.Unlock()
	if len(ops.completed) != len(tasks) {
		t.Fatalf("replay acknowledgements=%d", len(ops.completed))
	}
}
