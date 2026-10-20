package agentrollout

import (
	"testing"
	"time"
)

func TestStatusReadsDisabledWithoutV15OrRecentProof(t *testing.T) {
	f := newFixture(t)
	disabled, err := New(Config{Database: f.db.DB, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("DROP TABLE agent_release_signature_state"); err != nil {
		t.Fatal(err)
	}
	status, err := disabled.Status(t.Context(), f.actor)
	if err != nil || status.Enabled || status.Configured || status.Ready || status.Reason != "disabled" {
		t.Fatal(status, err)
	}
	if _, err := disabled.Status(t.Context(), Actor{}); err != ErrForbidden {
		t.Fatal(err)
	}
}

func TestStatusDistinguishesReadyUnconfiguredAndUnavailable(t *testing.T) {
	f := newFixture(t)
	assertStatus(t, f, "ready")
	verifier := f.s.sigstore
	f.s.sigstore = nil
	assertStatus(t, f, "unconfigured")
	f.s.sigstore = verifier
	key := f.s.keys["root"]
	expired := key
	expired.ExpiresAt = f.now
	f.s.keys["root"] = expired
	assertStatus(t, f, "unavailable")
	f.s.keys["root"] = key
	if _, err := f.db.DB.Exec("UPDATE agent_release_trust SET revoked_at=?", f.now); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f, "unavailable")
	if _, err := f.db.DB.Exec("UPDATE agent_release_signature_state SET policy_revision='missing'"); err == nil {
		t.Fatal("foreign key unexpectedly accepted")
	}
	if _, err := f.db.DB.Exec("DELETE FROM agent_release_signature_state"); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, f, "unavailable")
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Status(t.Context(), f.actor); err != ErrUnavailable {
		t.Fatal(err)
	}
}

func assertStatus(t *testing.T, f *fixture, reason string) {
	t.Helper()
	value, err := f.s.Status(t.Context(), f.actor)
	if err != nil || value.Reason != reason || value.Ready != (reason == "ready") || value.Configured != (reason != "unconfigured") || !value.Enabled {
		t.Fatal(value, err)
	}
}

func TestTargetObservationTimesAreStoredValues(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	before, err := f.s.ListTargets(t.Context(), f.actor, PageQuery{RolloutID: rollout.ID})
	if err != nil {
		t.Fatal(err)
	}
	if before.ServerTime == nil || !before.ServerTime.Equal(f.now) || before.Items[0].HealthySince != nil || before.Items[0].LastReportAt != nil || before.Items[0].LeaseExpiresAt != nil {
		t.Fatal(before)
	}
	since, report, expiry := f.now.Add(-time.Minute), f.now.Add(-time.Second), f.now.Add(time.Minute)
	if _, err := f.db.DB.Exec("UPDATE agent_rollout_targets SET healthy_since=?,last_report_at=?,lease_expires_at=? WHERE id=?", since, report, expiry, targets[0].ID); err != nil {
		t.Fatal(err)
	}
	page, err := f.s.ListTargets(t.Context(), f.actor, PageQuery{RolloutID: rollout.ID})
	if err != nil {
		t.Fatal(err)
	}
	item := page.Items[0]
	if item.HealthySince == nil || !item.HealthySince.Equal(since) || item.LastReportAt == nil || !item.LastReportAt.Equal(report) || item.LeaseExpiresAt == nil || !item.LeaseExpiresAt.Equal(expiry) {
		t.Fatal(item)
	}
	detail, items, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || detail.ServerTime == nil || !detail.ServerTime.Equal(f.now) || !items[0].HealthySince.Equal(since) {
		t.Fatal(detail, items, err)
	}
	current, err := f.s.GetRollout(t.Context(), f.actor, rollout.ID)
	if err != nil || current.ServerTime == nil || !current.ServerTime.Equal(f.now) {
		t.Fatal(current, err)
	}
}
