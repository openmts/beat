package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func resolverFixture(t *testing.T) (*TrafficAccountingResolver, *TrafficCalibrationStore, time.Time) {
	t.Helper()
	metrics := metricQueryMTS(t)
	sqlite := setupTestDB(t)
	t.Cleanup(func() { _ = sqlite.Close() })
	base := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	nodeID := "11111111-1111-1111-1111-111111111111"
	if _, err := sqlite.DB.ExecContext(t.Context(),
		`INSERT INTO nodes (id, name, host, port, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		nodeID, "node", "192.0.2.1", 22, base, base); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	samples := []struct {
		offset   time.Duration
		received float64
	}{{0, 100}, {time.Second, 150}, {2 * time.Second, 200}, {3 * time.Second, 260}}
	for _, sample := range samples {
		input := ingestInput(base.Add(sample.offset), sample.received)
		input.Identity.NodeID = nodeID
		if acceptIngest(t, metrics, input) {
			t.Fatalf("sample at %v reported duplicate", sample.offset)
		}
	}
	ledger := NewTrafficCalibrationStore(sqlite.DB)
	return NewTrafficAccountingResolver(metrics, ledger), ledger, base
}

func TestTrafficAccountingResolverRawCalibratedAndRevoked(t *testing.T) {
	resolver, ledger, base := resolverFixture(t)
	node := model.Node{ID: "11111111-1111-1111-1111-111111111111", Name: "node", TrafficResetDay: 1}
	now := base.Add(time.Hour)

	totals, accounting, err := resolver.CycleTotals(t.Context(), node, now)
	if err != nil || accounting.Mode != model.TrafficAccountingRaw ||
		totals.Received != 160 || totals.Sent != 80 {
		t.Fatalf("raw totals = %#v %#v %v", totals, accounting, err)
	}

	effective := base.Add(time.Second)
	set, err := ledger.AppendSet(t.Context(), TrafficCalibrationSetWrite{
		Request: model.TrafficCalibrationRequest{RequestID: calibrationTestUUID(1), TargetSent: 1000,
			TargetReceived: 2000, Reason: "opening balance"},
		NodeID: node.ID, NodeName: node.Name, EffectiveAt: effective,
		Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"}, Now: base.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("append calibration: %v", err)
	}
	totals, accounting, err = resolver.CycleTotals(t.Context(), node, now)
	if err != nil || accounting.Mode != model.TrafficAccountingCalibrated ||
		accounting.CalibratedAt == nil || !accounting.CalibratedAt.Equal(effective) {
		t.Fatalf("calibrated accounting = %#v %v", accounting, err)
	}
	if totals.Received != 2110 || totals.Sent != 1055 {
		t.Fatalf("calibrated totals = %#v, want received 2110 sent 1055", totals)
	}

	if _, err := ledger.AppendRevoke(t.Context(), TrafficCalibrationRevokeWrite{
		RequestID: calibrationTestUUID(2), NodeName: node.Name,
		Window:  TrafficCalibrationWindow{NodeID: node.ID, Start: base, End: base.AddDate(0, 1, 0)},
		EventID: set.Event.ID, Actor: model.TrafficCalibrationActor{ID: "user-1", Name: "owner"},
		Now: base.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("revoke calibration: %v", err)
	}
	totals, accounting, err = resolver.CycleTotals(t.Context(), node, now)
	if err != nil || accounting.Mode != model.TrafficAccountingRaw || totals.Received != 160 {
		t.Fatalf("post-revoke totals = %#v %#v %v", totals, accounting, err)
	}
}

func TestTrafficAccountingResolverPreviewAndNilHandling(t *testing.T) {
	resolver, _, base := resolverFixture(t)
	node := model.Node{ID: "11111111-1111-1111-1111-111111111111", Name: "node", TrafficResetDay: 1}
	preview, accounting, err := resolver.PreviewTotals(t.Context(), node, base.Add(2*time.Second), base.Add(time.Hour))
	if err != nil || accounting.Mode != model.TrafficAccountingCalibrated {
		t.Fatalf("preview = %#v %#v %v", preview, accounting, err)
	}
	if preview.Received != 100 || preview.Sent != 50 {
		t.Fatalf("preview totals = %#v, want received 100 sent 50", preview)
	}
	var empty *TrafficAccountingResolver
	totals, accounting, err := empty.CycleTotals(t.Context(), node, base)
	if err != nil || totals.Received != 0 || accounting.Mode != model.TrafficAccountingRaw {
		t.Fatalf("nil resolver = %#v %#v %v", totals, accounting, err)
	}
}

func TestTrafficAccountingResolverFailsWithoutCalibrationStore(t *testing.T) {
	metrics := metricQueryMTS(t)
	base := time.Now().UTC().Add(-time.Minute)
	if _, _, err := metrics.TrafficAccountingBoundary(t.Context(), "node"); err != nil {
		t.Fatalf("boundary query failed: %v", err)
	}
	resolver := NewTrafficAccountingResolver(metrics, nil)
	node := model.Node{ID: "node", Name: "node", TrafficResetDay: 1}
	_, accounting, err := resolver.CycleTotals(t.Context(), node, base)
	if err != nil || accounting.Mode != model.TrafficAccountingRaw {
		t.Fatalf("resolver without ledger = %#v %v", accounting, err)
	}
}
