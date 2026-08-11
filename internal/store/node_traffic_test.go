package store

import (
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestNodeStoreUpdatesTrafficPolicy(t *testing.T) {
	store := setupTestDB(t)
	nodes := NewNodeStore(store.DB)
	node, err := nodes.UpsertNode(t.Context(), "traffic-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	limit := int64(100 * 1024 * 1024)
	limitType := model.TrafficLimitMax
	resetDay := 31
	updated, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{
		Alias: "quota", GroupID: node.GroupID,
		TrafficLimit: &limit, TrafficLimitType: &limitType, TrafficResetDay: &resetDay,
	})
	if err != nil {
		t.Fatalf("update node traffic policy: %v", err)
	}
	if updated.TrafficLimit != limit || updated.TrafficLimitType != limitType ||
		updated.TrafficResetDay != resetDay {
		t.Fatalf("updated traffic policy = %#v", updated)
	}

	preserved, err := nodes.UpdateNode(t.Context(), node.ID, NodeUpdate{
		Alias: "preserved", GroupID: node.GroupID,
	})
	if err != nil {
		t.Fatalf("update node without traffic policy: %v", err)
	}
	if preserved.TrafficLimit != limit || preserved.TrafficLimitType != limitType ||
		preserved.TrafficResetDay != resetDay {
		t.Fatalf("traffic policy was not preserved: %#v", preserved)
	}
}

func TestNewNodesUseDefaultTrafficPolicy(t *testing.T) {
	store := setupTestDB(t)
	node, err := NewNodeStore(store.DB).UpsertNode(t.Context(), "default-traffic", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if node.TrafficLimit != 0 || node.TrafficLimitType != model.TrafficLimitSum || node.TrafficResetDay != 1 {
		t.Fatalf("default traffic policy = %#v", node)
	}
}
