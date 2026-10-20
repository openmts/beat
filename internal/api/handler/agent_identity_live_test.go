package handler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/service"
)

func TestAgentConnectionRejectsTasksAfterCredentialInvalidation(t *testing.T) {
	var revoked atomic.Bool
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(&fakeChannelOps{}, hub).WithIdentityVerifier(func(context.Context, model.AgentIdentity) error {
		if revoked.Load() {
			return errors.New("revoked")
		}
		return nil
	})
	conn := dialAgentChannel(t, handler)
	receiveAgentRefresh(t, conn)
	revoked.Store(true)
	if hub.PushTask(t.Context(), model.AgentTask{ID: "sensitive", NodeID: "node-1", Type: model.AgentTaskAuthorizedKey}) {
		t.Fatal("revoked credential accepted task")
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message service.TaskMessage
	if err := websocket.JSON.Receive(conn, &message); err == nil {
		t.Fatal("revoked connection received message")
	}
	awaitAgentOffline(t, hub)
}

func TestAgentConnectionPeriodicallyRechecksCredential(t *testing.T) {
	var revoked atomic.Bool
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(&fakeChannelOps{}, hub).WithIdentityVerifier(func(context.Context, model.AgentIdentity) error {
		if revoked.Load() {
			return errors.New("revoked")
		}
		return nil
	})
	conn := dialAgentChannel(t, handler)
	receiveAgentRefresh(t, conn)
	revoked.Store(true)
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message service.TaskMessage
	if err := websocket.JSON.Receive(conn, &message); err == nil {
		t.Fatal("revoked idle connection survived")
	}
	awaitAgentOffline(t, hub)
}

func TestAgentConnectionRejectsStaleHandshake(t *testing.T) {
	hub := service.NewAgentHub()
	handler := NewAgentChannelHandler(&fakeChannelOps{}, hub).WithIdentityVerifier(func(context.Context, model.AgentIdentity) error { return errors.New("revoked") })
	conn := dialAgentChannel(t, handler)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var message service.TaskMessage
	if err := websocket.JSON.Receive(conn, &message); err == nil {
		t.Fatal("stale handshake replayed tasks")
	}
	if hub.Online("node-1") {
		t.Fatal("stale handshake registered")
	}
	if agentCredentialBinding(model.AgentIdentity{Mode: model.AgentCredentialLegacy}) != "legacy" {
		t.Fatal("legacy binding")
	}
	if agentCredentialBinding(model.AgentIdentity{Mode: model.AgentCredentialActive, CredentialFingerprint: "fingerprint"}) != "active:fingerprint" {
		t.Fatal("active binding")
	}
	if (&agentWebSocketConn{binding: "epoch"}).CredentialBinding() != "epoch" {
		t.Fatal("connection binding")
	}
}
