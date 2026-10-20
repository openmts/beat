package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAcceleratorAuthorityAndPolicyFailures(t *testing.T) {
	f := newAcceleratorFixture(t)
	change := AcceleratorPolicyChange{Scope: AcceleratorPolicyScope{Kind: "node", ID: f.node.ID}, Write: model.AcceleratorPolicyWrite{SampleIntervalSeconds: 5, MaximumDevices: 32, Metrics: model.AcceleratorMetricKeys}}
	if _, err := f.inventory.SavePolicy(t.Context(), AcceleratorActor{}, change); err != ErrAcceleratorForbidden {
		t.Fatal(err)
	}
	if _, err := f.inventory.Policy(t.Context(), f.actor, AcceleratorPolicyScope{Kind: "bad", ID: "x"}); err != ErrAcceleratorPolicy {
		t.Fatal(err)
	}
	if _, err := f.inventory.Policy(t.Context(), f.actor, AcceleratorPolicyScope{Kind: "enrollment_policy", ID: "bad"}); err != ErrAcceleratorPolicy {
		t.Fatal(err)
	}
	if _, err := f.inventory.Policy(t.Context(), f.actor, AcceleratorPolicyScope{Kind: "node", ID: "missing"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	f.execGPU(t, `UPDATE admin_users SET role='admin' WHERE id=?`, f.actor.UserID)
	if _, err := f.inventory.Policy(t.Context(), f.actor, AcceleratorPolicyScope{Kind: "enrollment_policy", ID: "default"}); err != ErrAcceleratorForbidden {
		t.Fatal(err)
	}
	if _, err := f.inventory.Policy(t.Context(), f.actor, change.Scope); err != nil {
		t.Fatal(err)
	}
	f.execGPU(t, `UPDATE admin_sessions SET reauthenticated_until=NULL WHERE id=?`, f.actor.SessionID)
	if _, err := f.inventory.SavePolicy(t.Context(), f.actor, change); err != ErrAdminMutationRecent {
		t.Fatal(err)
	}
}

func (f *acceleratorFixture) execGPU(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.database.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestAcceleratorWritesDoNotSucceedWhenIgnored(t *testing.T) {
	for _, table := range []string{"accelerator_ingest_pending", "node_accelerator_state", "node_accelerators", "accelerator_collection_policies"} {
		t.Run(table, func(t *testing.T) {
			f := newAcceleratorFixture(t)
			report := f.report()
			if table == "accelerator_ingest_pending" {
				f.execGPU(t, `CREATE TRIGGER ignore_write BEFORE INSERT ON accelerator_ingest_pending BEGIN SELECT RAISE(IGNORE);END`)
				_, err := f.nodes.AcceptAgentContact(t.Context(), AgentContact{Identity: f.identity, Heartbeat: NodeHeartbeat{Host: "host", Port: 22}, Accelerators: report.Accelerators, ReportID: report.SampleID, ReportCapturedAt: report.CapturedAt})
				if err == nil {
					t.Fatal("ignored pending accepted")
				}
				return
			}
			if table == "accelerator_collection_policies" {
				f.execGPU(t, `CREATE TRIGGER ignore_write BEFORE INSERT ON accelerator_collection_policies BEGIN SELECT RAISE(IGNORE);END`)
				_, err := f.inventory.SavePolicy(t.Context(), f.actor, AcceleratorPolicyChange{Scope: AcceleratorPolicyScope{Kind: "node", ID: f.node.ID}, Write: model.AcceleratorPolicyWrite{SampleIntervalSeconds: 5, MaximumDevices: 32, Metrics: model.AcceleratorMetricKeys}})
				if err == nil {
					t.Fatal("ignored policy accepted")
				}
				return
			}
			attempt := f.prepare(t, report)
			if _, err := attempt.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			attempt.Close()
			f.execGPU(t, fmt.Sprintf(`CREATE TRIGGER ignore_write BEFORE INSERT ON %s BEGIN SELECT RAISE(IGNORE);END`, table))
			if err := f.publish(t); err == nil {
				t.Fatal("ignored inventory accepted")
			}
		})
	}
}

func TestAcceleratorMissingAndUncommittedReadPaths(t *testing.T) {
	f := newAcceleratorFixture(t)
	if state, err := f.inventory.State(t.Context(), f.node.ID); err != nil || state != nil {
		t.Fatal(state, err)
	}
	if pending, err := f.inventory.Pending(t.Context(), f.node.ID); err != nil || pending != nil {
		t.Fatal(pending, err)
	}
	if ids, err := f.inventory.PendingNodes(t.Context()); err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	if enabled, err := f.inventory.Enabled(t.Context()); err != nil || enabled {
		t.Fatal(enabled, err)
	}
	if err := f.inventory.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.inventory.AuthorizeRead(t.Context(), f.actor, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if public, err := f.inventory.PublicNode(t.Context(), "missing"); err != nil || public {
		t.Fatal(public, err)
	}
	report := f.report()
	attempt := f.prepare(t, report)
	attempt.Close()
	pending, err := f.inventory.Pending(t.Context(), f.node.ID)
	if err != nil || pending == nil {
		t.Fatal(pending, err)
	}
	if enabled, err := f.inventory.Enabled(t.Context()); err != nil || !enabled {
		t.Fatal("pending installation not enabled", enabled, err)
	}
	stats, err := f.inventory.Stats(t.Context())
	if err != nil || stats.Pending != 1 || stats.OldestPendingAt == nil {
		t.Fatal(stats, err)
	}
	if err = f.inventory.DiscardPending(t.Context(), *pending); err != nil {
		t.Fatal(err)
	}
}

func TestAcceleratorPolicyAndIdentityCapsAreEnforcedBeforeMTS(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	policy := model.DefaultAcceleratorPolicy()
	policy.Enabled = false
	if err := validateAcceleratorAcceptance(*report.Accelerators, policy); err != ErrAcceleratorPolicy {
		t.Fatal(err)
	}
	policy.Enabled = true
	policy.Configured = true
	policy.Revision = 1
	report.Accelerators.PolicyRevision = 1
	if err := validateAcceleratorAcceptance(*report.Accelerators, policy); err != ErrAcceleratorPolicy {
		t.Fatal("wrong cadence", err)
	}
	policy.SampleIntervalSeconds = 5
	policy.Metrics = []string{"gpu_utilization_percent"}
	if err := validateAcceleratorAcceptance(*report.Accelerators, policy); err != ErrAcceleratorPolicy {
		t.Fatal("denied metric", err)
	}
	policy.MaximumDevices = 0
	if err := validateAcceleratorAcceptance(*report.Accelerators, policy); err != ErrAcceleratorCapacity {
		t.Fatal(err)
	}
	for index := 1; index <= MaximumAcceleratorInventory; index++ {
		f.execGPU(t, `INSERT INTO node_accelerators(node_id,fingerprint,device_id,ordinal,vendor,model,capabilities_json,first_seen_at,last_seen_at,inactive_at) VALUES(?,?,?,?,'nvidia','retired','[]',?,?,?)`, f.node.ID, fmt.Sprintf("%064x", index), fmt.Sprintf("%032x", index), index, f.now, f.now, f.now)
	}
	report.Accelerators.PolicyRevision = 0
	_, err := f.nodes.AcceptAgentContact(t.Context(), AgentContact{Identity: f.identity, Heartbeat: NodeHeartbeat{Host: "host", Port: 22}, Accelerators: report.Accelerators, ReportID: report.SampleID, ReportCapturedAt: report.CapturedAt})
	if err != ErrAcceleratorCapacity {
		t.Fatal("unbounded device identity churn", err)
	}
}

func TestAcceleratorReceiptCannotPublishAnotherNodeInventory(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	attempt := f.prepare(t, report)
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	pending, err := f.inventory.Pending(t.Context(), f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := f.metrics.LatestAcceleratorSample(t.Context(), f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	receipt.NodeID = "another-node"
	if err = f.inventory.Publish(t.Context(), AcceleratorPublication{Pending: *pending, Receipt: *receipt}); err != ErrAgentSampleConflict {
		t.Fatal(err)
	}
}

func TestAcceleratorRetentionKeepsNativeProofForLongestGPUHistory(t *testing.T) {
	f := newAcceleratorFixture(t)
	f.execGPU(t, `UPDATE metric_definitions SET retention_days=365 WHERE metric_key='gpu_utilization_percent'`)
	cutoff := time.Now().AddDate(0, 0, -7)
	plans, err := f.metrics.metricRetentionPlan(t.Context(), cutoff)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, plan := range plans {
		if plan.measurement == model.AcceleratorSampleMeasurement {
			found = true
			if !plan.cutoff.Before(cutoff.AddDate(0, 0, -300)) {
				t.Fatal("GPU proof shorter than values")
			}
		}
	}
	if !found {
		t.Fatal("native receipt missing from retention")
	}
}

func TestAcceleratorClosedStoreRejectsReads(t *testing.T) {
	f := newAcceleratorFixture(t)
	if err := f.database.Close(); err != nil {
		t.Fatal(err)
	}
	calls := []func() error{
		func() error {
			_, err := f.inventory.Policy(t.Context(), f.actor, AcceleratorPolicyScope{Kind: "node", ID: f.node.ID})
			return err
		},
		func() error { _, err := f.inventory.State(t.Context(), f.node.ID); return err }, func() error { _, err := f.inventory.Devices(t.Context(), f.node.ID); return err },
		func() error { _, err := f.inventory.Pending(t.Context(), f.node.ID); return err }, func() error { _, err := f.inventory.PendingNodes(t.Context()); return err },
		func() error { _, err := f.inventory.Enabled(t.Context()); return err }, func() error { _, err := f.inventory.Stats(t.Context()); return err },
	}
	for _, call := range calls {
		if err := call(); err == nil {
			t.Fatal("closed store returned success")
		}
	}
	if err := acceleratorExactlyOne(nil, nil); err == nil {
		t.Fatal("missing write result accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := MigrateAccelerators(ctx, f.database.DB); err == nil {
		t.Fatal("canceled migration")
	}
	if _, err := f.nodes.MetricDevice(t.Context(), model.AcceleratorDeviceSelection{DeviceID: strings.Repeat("x", 32)}); err == nil {
		t.Fatal("invalid device selector")
	}
}
