package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/api/middleware"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func customMetricDeliveryFixture(t *testing.T) (*AgentChannelService, *store.CustomMetricStore, string) {
	t.Helper()
	sqlite, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	node, err := store.NewNodeStore(sqlite.DB).UpsertNode(t.Context(), "delivery", "127.0.0.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	metrics := store.NewCustomMetricStore(sqlite.DB)
	channel := NewAgentChannelService(store.NewAgentTaskStore(sqlite.DB)).WithCustomMetrics(metrics)
	return channel, metrics, node.ID
}

func TestCustomMetricDeliveryFailurePreservesPendingSnapshotForReconnect(t *testing.T) {
	channel, metrics, nodeID := customMetricDeliveryFixture(t)
	task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewAgentHub()
	failed := &fakeAgentConn{writeErr: errors.New("connection lost")}
	hub.Register(nodeID, failed)
	dispatcher := NewCustomMetricDispatchService(channel, hub)
	if err := dispatcher.DispatchCustomMetricTasks(t.Context(), []model.AgentTask{*task}); err != nil {
		t.Fatal(err)
	}
	if hub.Online(nodeID) || !failed.closed {
		t.Fatal("failed connection was retained")
	}
	pending, err := channel.RegisterAgent(t.Context(), nodeID)
	if err != nil || len(pending) != 1 || pending[0].ID != task.ID {
		t.Fatal("failed delivery lost its durable snapshot")
	}
	reconnected := &fakeAgentConn{}
	hub.Register(nodeID, reconnected)
	if err := dispatcher.DispatchCustomMetricTasks(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	if reconnected.written() != 1 {
		t.Fatal("reconnected node did not receive pending snapshot")
	}
	ctx := middleware.WithAgentIdentity(t.Context(), model.AgentIdentity{NodeID: nodeID})
	if err := channel.CompleteAgentTask(ctx, model.AgentTaskResult{TaskID: task.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	remaining, err := channel.RegisterAgent(t.Context(), nodeID)
	if err != nil || len(remaining) != 0 {
		t.Fatal("acknowledged replay stayed pending")
	}
}

type customContextConn struct {
	err   error
	calls int
}

func (conn *customContextConn) WriteTask(ctx context.Context, _ model.AgentTask) error {
	conn.calls++
	conn.err = ctx.Err()
	return conn.err
}

func (*customContextConn) Close() error { return nil }

func TestCustomMetricPersistedDeliverySurvivesRequestCancellation(t *testing.T) {
	channel, metrics, nodeID := customMetricDeliveryFixture(t)
	task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewAgentHub()
	conn := &customContextConn{}
	hub.Register(nodeID, conn)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := NewCustomMetricDispatchService(channel, hub).DispatchCustomMetricTasks(ctx, []model.AgentTask{*task}); err != nil {
		t.Fatal(err)
	}
	if conn.calls != 1 || conn.err != nil {
		t.Fatal("request cancellation prevented persisted delivery")
	}
	if err := NewCustomMetricDispatchService(channel, nil).DispatchCustomMetricTasks(ctx, []model.AgentTask{*task}); err != nil {
		t.Fatal(err)
	}
	if err := NewCustomMetricDispatchService(NewAgentChannelService(channel.tasks), hub).RegisterNodeCustomMetrics(t.Context(), nodeID); err == nil {
		t.Fatal("queue failure was swallowed")
	}
}

func TestCustomMetricServiceAcknowledgementRejectsDifferentNode(t *testing.T) {
	channel, metrics, nodeID := customMetricDeliveryFixture(t)
	task, err := metrics.QueueNodeCustomMetrics(t.Context(), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := middleware.WithAgentIdentity(t.Context(), model.AgentIdentity{NodeID: "different-node"})
	if err := channel.CompleteAgentTask(ctx, model.AgentTaskResult{TaskID: task.ID, Success: true}); err == nil {
		t.Fatal("service accepted another node's task result")
	}
}
