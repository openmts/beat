package agentrollout

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
	"github.com/beat/backend/internal/model"
)

func TestRestorePausesOnlyPrivateSnapshotAndRequiresReverification(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "restore.db")
	if err := f.db.Snapshot(t.Context(), path); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRestore(t.Context(), path, f.now); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var state, reason string
	var verified int
	if err := db.QueryRow("SELECT state,reason FROM agent_rollouts WHERE id=?", rollout.ID).Scan(&state, &reason); err != nil || state != "paused" || reason != "restored" {
		t.Fatal(state, reason, err)
	}
	if err := db.QueryRow("SELECT verified FROM agent_releases").Scan(&verified); err != nil || verified != 0 {
		t.Fatal(verified, err)
	}
	if err := db.QueryRow("SELECT state FROM agent_release_artifacts").Scan(&state); err != nil || state != "missing" {
		t.Fatal(state, err)
	}
	if err := db.QueryRow("SELECT state FROM agent_rollout_attempts").Scan(&state); err != nil || state != "unknown" {
		t.Fatal(state, err)
	}
	live, _, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || live.State != "active" {
		t.Fatal("restore mutated live state", err)
	}
}

func TestExpiredAssignmentPausesInsteadOfRedispatch(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(err)
	}
	f.now = assignment.ExpiresAt.Add(time.Second)
	if err := f.s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, targets, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || current.State != "paused" || targets[0].Reason != "assignment_unknown" {
		t.Fatal(current, targets, err)
	}
	if err := f.s.Resume(t.Context(), f.actor, ResumeInput{ID: rollout.ID}); err != ErrConflict {
		t.Fatal("uncertain update resumed without acknowledgment", err)
	}
	if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err != nil || output != nil {
		t.Fatal("unknown update automatically redispatched", err)
	}
}

func TestDisabledDefaultNeverReadsTablesOrCreatesAssets(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	service, err := New(Config{Database: db})
	if err != nil || service.Enabled() {
		t.Fatal(err)
	}
	if _, err := service.Lease(context.Background(), model.AgentIdentity{}, protocol.Capabilities{}); err != ErrDisabled {
		t.Fatal(err)
	}
	if err := service.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
}
