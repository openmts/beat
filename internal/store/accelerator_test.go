package store

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/testutil"
)

type acceleratorFixture struct {
	database  *SQLiteStore
	nodes     *NodeStore
	inventory *AcceleratorStore
	metrics   *MTSStore
	node      *model.Node
	identity  model.AgentIdentity
	actor     AcceleratorActor
	now       time.Time
}

func newAcceleratorFixture(t *testing.T) *acceleratorFixture {
	t.Helper()
	database := setupTestDB(t)
	metrics := metricQueryMTS(t)
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(database.DB)); err != nil {
		t.Fatal(err)
	}
	f := &acceleratorFixture{database: database, nodes: NewNodeStore(database.DB), inventory: NewAcceleratorStore(database.DB), metrics: metrics, now: time.Now().UTC(), actor: AcceleratorActor{UserID: "gpu-owner", SessionID: "gpu-session", RequestID: "gpu-request", IPAddress: "192.0.2.10", UserAgent: "GPU fixture"}}
	node, err := f.nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "gpu-node", Host: "host", Port: 22}, AgentCredential{Hash: bytes.Repeat([]byte{7}, 32), Prefix: "gpu", CreatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	f.node = node
	f.identity = model.AgentIdentity{NodeID: node.ID, Mode: model.AgentCredentialActive, CredentialFingerprint: model.AgentCredentialFingerprint(node.AgentTokenHash)}
	admins := NewAdminStore(database.DB)
	until := f.now.Add(time.Hour)
	if err = admins.CreateUser(t.Context(), &model.AdminUser{ID: f.actor.UserID, Username: "gpu-owner", DisplayName: "GPU owner", Role: "owner", Enabled: true, PasswordHash: "fixture", PasswordChangedAt: f.now, CreatedAt: f.now, UpdatedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	if err = admins.CreateSession(t.Context(), &model.AdminSession{ID: f.actor.SessionID, UserID: f.actor.UserID, TokenHash: bytes.Repeat([]byte{8}, 32), TokenPrefix: "gpu-session-prefix", CreatedAt: f.now, LastActivityAt: f.now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *acceleratorFixture) report() model.AgentReport {
	gpu := testutil.AcceleratorReport(f.now.Add(-time.Second))
	return model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: f.now, ReportIntervalMS: 1000, Host: "host", Port: 22, Metrics: &model.NodeMetrics{}, Accelerators: &gpu}
}

func (f *acceleratorFixture) prepare(t *testing.T, report model.AgentReport) *AgentIngestAttempt {
	t.Helper()
	attempt, err := f.metrics.PrepareAgentReport(t.Context(), AgentReportInput{Identity: f.identity, Report: report, Now: f.now}, func(ctx context.Context) (MetricReportSample, error) {
		return NewMetricCatalogStore(f.database.DB).PrepareAgentPolicy(ctx, AgentMetricPolicyInput{NodeID: f.node.ID})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(attempt.Close)
	_, err = f.nodes.AcceptAgentContact(t.Context(), AgentContact{Identity: f.identity, Heartbeat: NodeHeartbeat{Host: report.Host, Port: report.Port}, Accelerators: report.Accelerators, ReportID: report.SampleID, ReportCapturedAt: report.CapturedAt})
	if err != nil {
		attempt.Close()
		t.Fatal(err)
	}
	return attempt
}

func (f *acceleratorFixture) publish(t *testing.T) error {
	t.Helper()
	pending, err := f.inventory.Pending(t.Context(), f.node.ID)
	if err != nil {
		return err
	}
	if pending == nil {
		t.Fatal("no pending inventory")
	}
	receipt, err := f.metrics.AcceleratorSampleAt(t.Context(), f.node.ID, pending.Snapshot.CapturedAt.UnixNano())
	if err != nil {
		return err
	}
	if receipt == nil {
		t.Fatal("no durable MTS sample")
	}
	return f.inventory.Publish(t.Context(), AcceleratorPublication{Pending: *pending, Receipt: *receipt})
}

func TestAcceleratorAtomicMTSBeforeInventoryAndRetry(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	attempt := f.prepare(t, report)
	devices, err := f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || len(devices) != 0 {
		t.Fatal("inventory active before MTS", devices, err)
	}
	if _, err = attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	if err = f.publish(t); err != nil {
		t.Fatal(err)
	}
	devices, err = f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || len(devices) != 1 || devices[0].Ordinal != 1 {
		t.Fatal(devices, err)
	}
	values, err := f.metrics.AcceleratorValues(t.Context(), AcceleratorValueQuery{NodeID: f.node.ID, Snapshot: report.Accelerators.Inventory()})
	if err != nil || values[devices[0].DeviceID]["gpu_utilization_percent"] != 73.125 {
		t.Fatal(values, err)
	}
	repeat := f.prepare(t, report)
	if !repeat.Duplicate() {
		t.Fatal("retry not recognized")
	}
	if _, err = repeat.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	repeat.Close()
	if err = f.publish(t); err != nil {
		t.Fatal(err)
	}
	devices, err = f.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || len(devices) != 1 || devices[0].Ordinal != 1 {
		t.Fatal("retry changed inventory", devices, err)
	}
	var encoded string
	if err = f.database.DB.QueryRow(`SELECT inventory_json FROM node_accelerator_state WHERE node_id=?`, f.node.ID).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "73.125") || strings.Contains(encoded, `"metrics":{`) {
		t.Fatal("numeric GPU samples reached SQLite")
	}
}

func TestAcceleratorInventoryFailureReplaysDurableSnapshot(t *testing.T) {
	f := newAcceleratorFixture(t)
	report := f.report()
	attempt := f.prepare(t, report)
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	attempt.Close()
	if _, err := f.database.DB.Exec(`CREATE TRIGGER fail_gpu_inventory BEFORE INSERT ON node_accelerators BEGIN SELECT RAISE(ABORT,'fixture');END`); err != nil {
		t.Fatal(err)
	}
	if err := f.publish(t); err == nil {
		t.Fatal("inventory failure accepted")
	}
	pending, err := f.inventory.Pending(t.Context(), f.node.ID)
	if err != nil || pending == nil {
		t.Fatal("lost replay state", err)
	}
	if _, err = f.database.DB.Exec(`DROP TRIGGER fail_gpu_inventory`); err != nil {
		t.Fatal(err)
	}
	if err = f.publish(t); err != nil {
		t.Fatal(err)
	}
	pending, err = f.inventory.Pending(t.Context(), f.node.ID)
	if err != nil || pending != nil {
		t.Fatal("replay not cleared", err)
	}
}

func TestAcceleratorPolicyORExplicitDisableAndAtomicAudit(t *testing.T) {
	f := newAcceleratorFixture(t)
	policy, err := f.inventory.AgentPolicy(t.Context(), f.node.ID, true)
	if err != nil || !policy.Enabled || policy.Configured || policy.EffectiveSource != "installation" {
		t.Fatal(policy, err)
	}
	change := AcceleratorPolicyChange{Scope: AcceleratorPolicyScope{Kind: "node", ID: f.node.ID}, Write: model.AcceleratorPolicyWrite{SampleIntervalSeconds: 10, MaximumDevices: 32, Metrics: model.AcceleratorMetricKeys}}
	policy, err = f.inventory.SavePolicy(t.Context(), f.actor, change)
	if err != nil || !policy.Configured || policy.Enabled {
		t.Fatal(policy, err)
	}
	policy, err = f.inventory.AgentPolicy(t.Context(), f.node.ID, true)
	if err != nil || policy.Enabled {
		t.Fatal("explicit disable lost", policy, err)
	}
	var event model.AdminAuditEvent
	err = f.database.DB.QueryRow(`SELECT request_id,actor_username,ip_address,user_agent,session_prefix FROM admin_audit_events WHERE action='accelerator.policy.update'`).Scan(&event.RequestID, &event.ActorUsername, &event.IPAddress, &event.UserAgent, &event.SessionPrefix)
	if err != nil || event.RequestID != f.actor.RequestID || event.IPAddress != f.actor.IPAddress || event.SessionPrefix != "gpu-session-prefix" || event.UserAgent != f.actor.UserAgent {
		t.Fatal(event, err)
	}
	if _, err = f.database.DB.Exec(`CREATE TRIGGER ignore_gpu_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE);END`); err != nil {
		t.Fatal(err)
	}
	change.Write.ExpectedRevision = 1
	change.Write.Enabled = true
	if _, err = f.inventory.SavePolicy(t.Context(), f.actor, change); err == nil {
		t.Fatal("audit-ignore mutation succeeded")
	}
	policy, err = f.inventory.Policy(t.Context(), f.actor, change.Scope)
	if err != nil || policy.Revision != 1 || policy.Enabled {
		t.Fatal("audit failure did not rollback", policy, err)
	}
}
