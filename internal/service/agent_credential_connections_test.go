package service

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

type boundAgentConn struct {
	fakeAgentConn
	binding string
}

func (conn *boundAgentConn) CredentialBinding() string { return conn.binding }

func TestAgentHubCredentialInvalidationPreservesNewConnection(t *testing.T) {
	hub := NewAgentHub()
	old := &boundAgentConn{binding: "active:old"}
	hub.Register("node", old)
	hub.DisconnectCredential("node", "active:other")
	if old.closed || !hub.Online("node") {
		t.Fatal("different epoch disconnected")
	}
	fresh := &boundAgentConn{binding: "active:new"}
	hub.Register("node", fresh)
	hub.DisconnectCredential("node", "active:old")
	if fresh.closed || !hub.Online("node") {
		t.Fatal("old mutation disconnected replacement")
	}
	hub.DisconnectCredential("node", "active:new")
	if !fresh.closed || hub.Online("node") {
		t.Fatal("current epoch remained online")
	}
	if hub.PushTask(context.Background(), model.AgentTask{NodeID: "node"}) {
		t.Fatal("disconnected epoch received task")
	}
	hub.DisconnectCredential("missing", "")
	hub.Register("unbound", &fakeAgentConn{})
	hub.DisconnectCredential("unbound", "")
	if !hub.Online("unbound") {
		t.Fatal("unrelated connection removed")
	}
}
