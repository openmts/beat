package accelerators

import (
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestPolicyWrappersAndExplicitDisableOverridePending(t *testing.T) {
	f := newFixture(t)
	report := f.stage(t, true)
	if err := f.service.PublishReport(t.Context(), f.node.ID, "other-report"); err != nil {
		t.Fatal(err)
	}
	policy, err := f.service.AgentPolicy(t.Context(), f.node.ID, true)
	if err != nil || !policy.Enabled {
		t.Fatal(policy, err)
	}
	change := store.AcceleratorPolicyChange{Scope: store.AcceleratorPolicyScope{Kind: "node", ID: f.node.ID}, Write: model.AcceleratorPolicyWrite{SampleIntervalSeconds: 5, MaximumDevices: 32, Metrics: model.AcceleratorMetricKeys}}
	if _, err = f.service.SavePolicy(t.Context(), f.actor, change); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Policy(t.Context(), f.actor, change.Scope); err != nil {
		t.Fatal(err)
	}
	if err = f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err != nil {
		t.Fatal(err)
	}
	pending, err := f.service.inventory.Pending(t.Context(), f.node.ID)
	if err != nil || pending != nil {
		t.Fatal(pending, err)
	}
	if err = f.service.Ready(t.Context()); err != nil {
		t.Fatal("disabled pipeline degraded", err)
	}
}

func TestRevokedCredentialReconciliationCannotPublishInventory(t *testing.T) {
	f := newFixture(t)
	f.stage(t, true)
	if _, err := f.nodes.RevokeAgentToken(t.Context(), f.node.ID, f.at); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	devices, err := f.service.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || len(devices) != 0 {
		t.Fatal(devices, err)
	}
}

func TestReadErrorsRemainVisibleAndPrivateNodesHidden(t *testing.T) {
	f := newFixture(t)
	report := f.stage(t, true)
	if err := f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec(`UPDATE nodes SET is_public=0 WHERE id=?`, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if values, err := f.service.PublicAccelerators(t.Context(), f.node.ID); err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if values, err := f.service.PublicAccelerators(t.Context(), "missing"); err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if _, err := f.db.DB.Exec(`UPDATE nodes SET is_public=1 WHERE id=?`, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PublicAccelerators(t.Context(), f.node.ID); err == nil {
		t.Fatal("closed MTS hidden")
	}
	if _, err := f.service.Node(t.Context(), f.actor, f.node.ID); err == nil {
		t.Fatal("closed private MTS hidden")
	}
	if err := f.service.Ready(t.Context()); err == nil {
		t.Fatal("enabled failed MTS ready")
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Reconcile(t.Context()); err == nil {
		t.Fatal("closed inventory hidden")
	}
	if err := f.service.Ready(t.Context()); err == nil {
		t.Fatal("closed inventory ready")
	}
}

func TestMalformedPendingAndCatalogFailuresPropagate(t *testing.T) {
	f := newFixture(t)
	report := f.stage(t, true)
	if _, err := f.db.DB.Exec(`UPDATE accelerator_ingest_pending SET inventory_json='{}'`); err != nil {
		t.Fatal(err)
	}
	if err := f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err == nil {
		t.Fatal("invalid pending published")
	}
	if err := f.service.Reconcile(t.Context()); err == nil {
		t.Fatal("invalid pending hidden")
	}
}
