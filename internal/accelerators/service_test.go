package accelerators

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type fixture struct {
	service  *Service
	db       *store.SQLiteStore
	nodes    *store.NodeStore
	node     *model.Node
	identity model.AgentIdentity
	actor    store.AcceleratorActor
	at       time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	metrics, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metrics.Close() })
	catalog := store.NewMetricCatalogStore(database.DB)
	if err = metrics.AttachMetricCatalog(t.Context(), catalog); err != nil {
		t.Fatal(err)
	}
	f := &fixture{db: database, nodes: store.NewNodeStore(database.DB), at: time.Now().UTC(), actor: store.AcceleratorActor{UserID: "owner", SessionID: "session", RequestID: "request"}}
	f.node, err = f.nodes.CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "gpu", Host: "host", Port: 22}, store.AgentCredential{Hash: bytes.Repeat([]byte{7}, 32), Prefix: "gpu", CreatedAt: f.at})
	if err != nil {
		t.Fatal(err)
	}
	f.identity = model.AgentIdentity{NodeID: f.node.ID, Mode: model.AgentCredentialActive, CredentialFingerprint: model.AgentCredentialFingerprint(f.node.AgentTokenHash)}
	f.service, err = NewService(Config{Inventory: store.NewAcceleratorStore(database.DB), Metrics: metrics, Catalog: catalog, Now: func() time.Time { return f.at }})
	if err != nil {
		t.Fatal(err)
	}
	admins := store.NewAdminStore(database.DB)
	until := f.at.Add(time.Hour)
	if err = admins.CreateUser(t.Context(), &model.AdminUser{ID: "owner", Username: "owner", DisplayName: "Owner", Role: "owner", Enabled: true, PasswordHash: "fixture", PasswordChangedAt: f.at, CreatedAt: f.at, UpdatedAt: f.at}); err != nil {
		t.Fatal(err)
	}
	if err = admins.CreateSession(t.Context(), &model.AdminSession{ID: "session", UserID: "owner", TokenHash: bytes.Repeat([]byte{9}, 32), TokenPrefix: "prefix", CreatedAt: f.at, LastActivityAt: f.at, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) stage(t *testing.T, commit bool) model.AgentReport {
	t.Helper()
	gpu := testutil.AcceleratorReport(f.at.Add(-time.Second))
	report := model.AgentReport{ProtocolVersion: 2, SampleID: uuid.NewString(), CapturedAt: f.at, ReportIntervalMS: 1000, Host: "host", Port: 22, Metrics: &model.NodeMetrics{}, Accelerators: &gpu}
	attempt, err := f.service.metrics.PrepareAgentReport(t.Context(), store.AgentReportInput{Identity: f.identity, Report: report, Now: f.at}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	_, err = f.nodes.AcceptAgentContact(t.Context(), store.AgentContact{Identity: f.identity, Heartbeat: store.NodeHeartbeat{Host: "host", Port: 22}, Accelerators: &gpu, ReportID: report.SampleID, ReportCapturedAt: report.CapturedAt})
	if err != nil {
		t.Fatal(err)
	}
	if commit {
		if _, err = attempt.Commit(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	return report
}

func TestPublicationReadinessIncludesFirstInstallationPending(t *testing.T) {
	f := newFixture(t)
	report := f.stage(t, true)
	if _, err := f.db.DB.Exec(`CREATE TRIGGER fail_gpu_state BEFORE INSERT ON node_accelerator_state BEGIN SELECT RAISE(ABORT,'fail');END`); err != nil {
		t.Fatal(err)
	}
	if err := f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err == nil {
		t.Fatal("failed publication accepted")
	}
	if err := f.service.Ready(t.Context()); err == nil {
		t.Fatal("first opt-in pending hid persistence failure")
	}
	if _, err := f.db.DB.Exec(`DROP TRIGGER fail_gpu_state`); err != nil {
		t.Fatal(err)
	}
	if err := f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	stats, err := f.service.Stats(t.Context())
	if err != nil || stats.ActiveDevices != 1 || stats.Pending != 0 {
		t.Fatal(stats, err)
	}
}

func TestViewsRedactIdentityAndUseNativeTime(t *testing.T) {
	f := newFixture(t)
	report := f.stage(t, true)
	if err := f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err != nil {
		t.Fatal(err)
	}
	private, err := f.service.Node(t.Context(), f.actor, f.node.ID)
	if err != nil || len(private.Devices) != 1 || private.Devices[0].DriverVersion != "fixture" {
		t.Fatal(private, err)
	}
	if _, err = f.db.DB.Exec(`UPDATE nodes SET is_public=1 WHERE id=?`, f.node.ID); err != nil {
		t.Fatal(err)
	}
	public, err := f.service.PublicAccelerators(t.Context(), f.node.ID)
	if err != nil || len(public) != 1 || public[0].Metrics["gpu_utilization_percent"] != 73.125 {
		t.Fatal(public, err)
	}
	encoded, err := json.Marshal(public)
	if err != nil || strings.Contains(string(encoded), "fingerprint") || strings.Contains(string(encoded), "driver_version") {
		t.Fatal(string(encoded), err)
	}
	if !public[0].CapturedAt.Equal(report.Accelerators.CapturedAt) || public[0].CadenceMS != 5000 {
		t.Fatal("host heartbeat refreshed GPU time")
	}
	f.at = f.at.Add(time.Minute)
	public, err = f.service.PublicAccelerators(t.Context(), f.node.ID)
	if err != nil || !public[0].Stale {
		t.Fatal(public, err)
	}
	if _, err = f.db.DB.Exec(`UPDATE metric_definitions SET public=0 WHERE metric_key='gpu_temperature_celsius'`); err != nil {
		t.Fatal(err)
	}
	public, err = f.service.PublicAccelerators(t.Context(), f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := public[0].Metrics["gpu_temperature_celsius"]; found {
		t.Fatal("private GPU metric exposed")
	}
}

func TestUncommittedInventoryNeverPublishesAndExpires(t *testing.T) {
	f := newFixture(t)
	report := f.stage(t, false)
	if err := f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	values, err := f.service.inventory.Devices(t.Context(), f.node.ID)
	if err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if err = f.service.PublishReport(t.Context(), f.node.ID, report.SampleID); err != store.ErrAcceleratorUnavailable {
		t.Fatal(err)
	}
	f.at = f.at.Add(16 * time.Minute)
	if err = f.service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err := f.service.inventory.Pending(t.Context(), f.node.ID)
	if err != nil || pending != nil {
		t.Fatal(pending, err)
	}
}

func TestDisabledPipelineAndWorkerLifecycle(t *testing.T) {
	f := newFixture(t)
	if _, err := NewService(Config{}); err == nil {
		t.Fatal("missing dependencies")
	}
	if err := f.service.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Node(t.Context(), f.actor, f.node.ID)
	if err != nil || result.State != "disabled" || len(result.Devices) != 0 {
		t.Fatal(result, err)
	}
	if _, err = f.service.Node(t.Context(), store.AcceleratorActor{}, f.node.ID); err == nil {
		t.Fatal("anonymous private read")
	}
	changes := make(chan error, 2)
	f.service.SetObserver(func(err error) { changes <- err })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.service.Run(ctx) }()
	select {
	case err = <-changes:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not report")
	}
	cancel()
	if err = <-done; err != context.Canceled {
		t.Fatal(err)
	}
}
