package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"beat/internal/domain"
)

func TestAgentUpgradeBatchLifecycle(t *testing.T) {
	store, err := Open(t.TempDir() + "/beat.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	batch, err := store.CreateAgentUpgradeBatch(ctx, "0.2.0", []domain.AgentUpgradeItem{
		{AgentID: "agent-a", NodeID: "node-a", NodeName: "edge-a", Arch: "amd64", FromVersion: "0.1.0", ArtifactSHA256: "a", ArtifactSize: 10},
		{AgentID: "agent-b", NodeID: "node-b", NodeName: "edge-b", Arch: "arm64", FromVersion: "0.1.0", ArtifactSHA256: "b", ArtifactSize: 20},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Total != 2 || batch.Queued != 2 || batch.Items[0].ToVersion != "0.2.0" {
		t.Fatalf("unexpected batch: %+v", batch)
	}
	if _, err := store.CreateAgentUpgradeBatch(ctx, "0.2.0", []domain.AgentUpgradeItem{{AgentID: "agent-c"}}, now); !errors.Is(err, ErrAgentUpgradeInProgress) {
		t.Fatalf("second batch error = %v, want ErrAgentUpgradeInProgress", err)
	}

	item := batch.Items[0]
	delivered, err := store.MarkAgentUpgradeDelivered(ctx, item.ID, now.Add(time.Second))
	if err != nil || !delivered {
		t.Fatalf("MarkAgentUpgradeDelivered = %v, %v", delivered, err)
	}
	updated, err := store.UpdateAgentUpgradeProgress(ctx, item.AgentID, item.ID, "failed", "checksum mismatch", now.Add(2*time.Second))
	if err != nil || !updated {
		t.Fatalf("UpdateAgentUpgradeProgress = %v, %v", updated, err)
	}
	batch, err = store.GetAgentUpgradeBatch(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Failed != 1 || batch.Queued != 1 {
		t.Fatalf("unexpected failed batch: %+v", batch)
	}

	batch, err = store.RetryAgentUpgradeBatch(ctx, batch.ID, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Failed != 0 || batch.Queued != 2 || batch.Status != "running" {
		t.Fatalf("unexpected retried batch: %+v", batch)
	}
	for _, item := range batch.Items {
		if _, err := store.MarkAgentUpgradeDelivered(ctx, item.ID, now.Add(4*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	completed, err := store.ReconcileAgentUpgradeVersion(ctx, "agent-a", "0.2.0", now.Add(5*time.Second))
	if err != nil || len(completed) != 1 {
		t.Fatalf("ReconcileAgentUpgradeVersion = %v, %v", completed, err)
	}
	batch, err = store.GetAgentUpgradeBatch(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Succeeded != 1 || batch.Delivered != 1 || batch.Status != "running" {
		t.Fatalf("unexpected partial success batch: %+v", batch)
	}
}
