package handler

import (
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/service"
)

func receiveAgentRefresh(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var message service.TaskMessage
	if err := websocket.JSON.Receive(conn, &message); err != nil {
		t.Fatal(err)
	}
	if message.Task.ID != "refresh-node-1" {
		t.Fatal("new channel did not initialize")
	}
}

func awaitAgentOffline(t *testing.T, hub *service.AgentHub) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for hub.Online("node-1") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if hub.Online("node-1") {
		t.Fatal("closed channel waited for ping before unregistering")
	}
}

func TestAgentChannelDisconnectUnregistersWithoutWaitingForPing(t *testing.T) {
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(&fakeChannelOps{}, hub)
	for range 2 {
		conn := dialAgentChannel(t, handler)
		receiveAgentRefresh(t, conn)
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		awaitAgentOffline(t, hub)
	}
}

func TestAgentChannelReplacedConnectionDoesNotUnregisterNewConnection(t *testing.T) {
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(&fakeChannelOps{}, hub)
	first := dialAgentChannel(t, handler)
	receiveAgentRefresh(t, first)
	second := dialAgentChannel(t, handler)
	receiveAgentRefresh(t, second)
	var message service.TaskMessage
	if err := websocket.JSON.Receive(first, &message); err == nil {
		t.Fatal("replaced connection remained open")
	}
	if !hub.Online("node-1") {
		t.Fatal("old connection cleanup removed replacement")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	awaitAgentOffline(t, hub)
}
