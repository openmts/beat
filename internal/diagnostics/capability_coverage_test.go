//go:build linux

package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// fakeRow 让扫描函数可以在不构造 SQL 的情况下覆盖类型分支。
type fakeRow struct {
	values []any
	err    error
}

func (row fakeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("column count mismatch")
	}
	for index, value := range row.values {
		if value == nil {
			continue
		}
		slot := reflect.ValueOf(dest[index])
		if slot.Kind() != reflect.Pointer || slot.IsNil() {
			return errors.New("destination must be a pointer")
		}
		source := reflect.ValueOf(value)
		switch {
		case source.Type().AssignableTo(slot.Elem().Type()):
			slot.Elem().Set(source)
		case source.Type().ConvertibleTo(slot.Elem().Type()):
			slot.Elem().Set(source.Convert(slot.Elem().Type()))
		default:
			return errors.New("incompatible destination")
		}
	}
	return nil
}

func targetRowValues(kinds, allowed, resolved string) []any {
	return []any{"target", "name", "purpose", kinds, "93.184.216.34", 0, model.IPFamilyIPv4,
		model.DiagnosticProtocolICMP, model.DiagnosticDirectionForward, false, allowed, resolved, sql.NullTime{},
		100, 10, 1, int64(0), true, sql.NullTime{}, 1, "owner", "owner", time.Now(), time.Now()}
}

func TestScanTargetVariants(t *testing.T) {
	if _, err := scanTarget(fakeRow{err: sql.ErrNoRows}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("no rows error = %v", err)
	}
	if _, err := scanTarget(fakeRow{err: errors.New("boom")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("scan error = %v", err)
	}
	if _, err := scanTarget(fakeRow{values: targetRowValues("{", "[]", "[]")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("invalid json error = %v", err)
	}
	target, err := scanTarget(fakeRow{values: targetRowValues("null", "null", "null")})
	if err != nil || target.Kinds == nil || target.AllowedAddresses == nil || target.ResolvedAddresses == nil {
		t.Fatalf("null lists = %+v / %v", target, err)
	}
	resolved := sql.NullTime{Time: time.Now(), Valid: true}
	values := targetRowValues(`["mtr"]`, `["10.0.0.0/8"]`, `["93.184.216.34"]`)
	values[12] = resolved
	values[18] = resolved
	target, err = scanTarget(fakeRow{values: values})
	if err != nil || target.ResolvedAt == nil || target.ExpiresAt == nil || len(target.ResolvedAddresses) != 1 {
		t.Fatalf("populated target = %+v / %v", target, err)
	}
}

func TestScanAttemptVariants(t *testing.T) {
	if _, err := scanAttempt(fakeRow{err: sql.ErrNoRows}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no rows error = %v", err)
	}
	if _, err := scanAttempt(fakeRow{err: errors.New("boom")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("scan error = %v", err)
	}
	heartbeat := sql.NullTime{Time: time.Now(), Valid: true}
	values := []any{"job", 1, "running", "nonce", time.Now(), sql.NullTime{}, heartbeat}
	attempt, err := scanAttempt(fakeRow{values: values})
	if err != nil || attempt.StartedAt != nil || attempt.HeartbeatAt.IsZero() {
		t.Fatalf("attempt = %+v / %v", attempt, err)
	}
}

func TestCapabilityFreshnessAndRendering(t *testing.T) {
	f := newFixture(t)
	digest := advertise(t, f)
	if _, err := f.database.DB.Exec(`UPDATE diagnostic_capabilities SET seen_at=? WHERE node_id=?`,
		f.now.Add(-time.Hour), f.nodeID); err != nil {
		t.Fatal(err)
	}
	policy := enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	input := JobInput{SourceNodeID: f.nodeID, TargetID: target.ID, Kind: model.DiagnosticKindMTR,
		Protocol: model.DiagnosticProtocolICMP, Direction: model.DiagnosticDirectionForward,
		IPFamily: model.IPFamilyIPv4, HopLimit: 32}
	if _, err := f.service.CreateJob(context.Background(), f.owner, input); !errors.Is(err, ErrCapability) {
		t.Fatalf("stale capability error = %v", err)
	}
	if lease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: digest}); !errors.Is(err, ErrCapability) || lease != nil {
		t.Fatalf("stale lease = %+v / %v", lease, err)
	}
	if _, err := f.database.DB.Exec(`UPDATE diagnostic_capabilities SET seen_at=?, document_json='not json' WHERE node_id=?`,
		f.now, f.nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateJob(context.Background(), f.owner, input); !errors.Is(err, ErrCapability) {
		t.Fatalf("corrupt capability error = %v", err)
	}
	_ = policy

	now := f.now
	rows := []struct {
		id     string
		body   string
		seenAt time.Time
	}{
		{"ghost-invalid", "not json", now},
		{"ghost-partial", `{"version":1,"kinds":["mtr"],"state":"partial"}`, now},
		{"ghost-stale", `{"version":1,"kinds":["mtr"],"state":"ready"}`, now.Add(-time.Hour)},
		{"ghost-ready", `{"version":1,"kinds":["mtr"],"state":"ready"}`, now},
	}
	for _, row := range rows {
		if _, err := f.database.DB.Exec(`INSERT INTO diagnostic_capabilities(node_id, digest, document_json, seen_at)
			VALUES(?,?,?,?)`, row.id, "digest-"+row.id, row.body, row.seenAt); err != nil {
			t.Fatal(err)
		}
	}
	capabilities, err := f.service.Capabilities(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, capability := range capabilities {
		states[capability.NodeID] = capability.State
	}
	want := map[string]string{"ghost-invalid": "invalid", "ghost-partial": "partial",
		"ghost-stale": "stale", "ghost-ready": "ready"}
	for id, state := range want {
		if states[id] != state {
			t.Fatalf("state for %s = %q, want %q", id, states[id], state)
		}
	}
	if _, err := f.service.Capabilities(context.Background(), actorOf("session-unknown")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("capabilities actor error = %v", err)
	}
}

func TestLeaseAndDeleteJobBranches(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	digest := advertise(t, f)
	if lease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: digest}); err != nil || lease != nil {
		t.Fatalf("empty queue lease = %+v / %v", lease, err)
	}
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	job := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)
	if _, err := f.database.DB.Exec(`DELETE FROM diagnostic_targets WHERE id=?`, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token},
		CapabilityDigest: digest}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("deleted target lease error = %v", err)
	}

	f.service.metrics = nil
	if err := f.service.DeleteJob(context.Background(), f.owner, job.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil metrics error = %v", err)
	}
	f.service.metrics = f.metrics
	if err := f.service.DeleteJob(context.Background(), actorOf("session-unknown"), job.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unknown actor error = %v", err)
	}
	if err := f.service.DeleteJob(context.Background(), f.owner, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing job error = %v", err)
	}
	if err := f.service.DeleteJob(context.Background(), f.owner, job.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("active job error = %v", err)
	}
	if err := f.service.CancelJob(context.Background(), f.owner, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteJob(context.Background(), f.owner, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Job(context.Background(), f.owner, job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted job error = %v", err)
	}
}

func TestReconcileExpiresQueueAndLeases(t *testing.T) {
	f := newFixture(t)
	enableMTRPolicy(t, f, false)
	target := createMTRTarget(t, f, "coverage-target", "93.184.216.34", false)
	advertise(t, f)
	expired := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)
	if _, err := f.database.DB.Exec(`UPDATE diagnostic_jobs SET created_at=? WHERE id=?`,
		f.now.Add(-2*QueueLifetime), expired.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail, err := f.service.Job(context.Background(), f.owner, expired.ID)
	if err != nil || detail.Job.State != model.DiagnosticStateTimedOut {
		t.Fatalf("expired job = %+v / %v", detail.Job.State, err)
	}

	second := createMTRJob(t, f, model.DefaultDiagnosticPolicy(), target)
	digest, err := f.service.Advertise(context.Background(), NodeProof{Token: f.token}, validCoverageDocument())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := f.service.Lease(context.Background(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: digest})
	if err != nil || lease == nil {
		t.Fatalf("lease = %+v / %v", lease, err)
	}
	if _, err := f.database.DB.Exec(`UPDATE diagnostic_attempts SET leased_at=? WHERE job_id=?`,
		f.now.Add(-2*leaseDuration), second.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail, err = f.service.Job(context.Background(), f.owner, second.ID)
	if err != nil || detail.Job.State != model.DiagnosticStateQueued {
		t.Fatalf("recycled job = %+v / %v", detail.Job, err)
	}
}
