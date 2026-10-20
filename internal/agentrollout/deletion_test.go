package agentrollout

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteRolloutRejectsNonTerminalState(t *testing.T) {
	f := newFixture(t)
	rollout, _ := f.rollout(t, 2)
	if err := f.s.DeleteRollout(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRollout(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal(err)
	}
	if _, err := f.s.GetRollout(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRolloutRemovesDependentsAndAudits(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 2)
	if err := f.s.Abort(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRollout(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetRollout(t.Context(), f.actor, rollout.ID); err != ErrNotFound {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatal(targets)
	}
	for query, expected := range map[string]int{
		"SELECT COUNT(*) FROM agent_rollout_targets WHERE rollout_id=?":                                                            0,
		"SELECT COUNT(*) FROM agent_rollout_attempts WHERE target_id IN (SELECT id FROM agent_rollout_targets WHERE rollout_id=?)": 0,
		"SELECT COUNT(*) FROM agent_rollout_events WHERE rollout_id=?":                                                             0,
		"SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.delete' AND resource_id=?":                                  1,
	} {
		var count int
		if err := f.db.DB.QueryRow(query, rollout.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatal(query, count)
		}
	}
}

func TestDeleteRolloutRejectsInflightAssignments(t *testing.T) {
	f := newFixture(t)
	rollout, _ := f.rollout(t, 1)
	if err := f.s.Abort(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_rollout_targets SET state='offered' WHERE rollout_id=?", rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRollout(t.Context(), f.actor, rollout.ID); err != ErrConflict {
		t.Fatal(err)
	}
}

func TestDeleteReleaseRequiresRevocationAndDetachedRollouts(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	if err := f.s.DeleteRelease(t.Context(), f.actor, release.ID); err != ErrConflict {
		t.Fatal(err)
	}
	nodes := f.nodes(t, 1)
	rollout, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "Detach guard", ReleaseID: release.ID, NodeIDs: nodes, Policy: Policy{CanaryCount: 1, WaveSize: 1, ObservationSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRelease(t.Context(), f.actor, release.ID); err != ErrConflict {
		t.Fatal(err)
	}
	if err := f.s.Abort(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRollout(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReleaseDetail(t.Context(), f.actor, release.ID); err != ErrNotFound {
		t.Fatal(err)
	}
	digest := sha256.Sum256(f.artifact)
	path := filepath.Join(f.s.directory, hex.EncodeToString(digest[:])+artifactFileSuffix)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("artifact file survived release deletion", err)
	}
	var count int
	if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='release.delete' AND resource_id=?", release.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestDeleteReleaseKeepsArtifactSharedWithAnotherRelease(t *testing.T) {
	f := newFixture(t)
	first := f.release(t)
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = "2.1.0"
	f.sign(t, manifest)
	second := f.release(t)
	path := filepath.Join(f.s.directory, manifest.Artifacts[0].SHA256+artifactFileSuffix)
	for _, release := range []Release{first, second} {
		if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.s.DeleteRelease(t.Context(), f.actor, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("shared artifact file was removed", err)
	}
	if err := f.s.DeleteRelease(t.Context(), f.actor, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("last reference to the artifact file survived", err)
	}
}

func TestDeleteReleaseSkipsMalformedStoredDigest(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	if _, err := f.db.DB.Exec("UPDATE agent_release_artifacts SET digest='not-a-digest' WHERE release_id=?", release.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRejectsInvalidAndUnknownIdentifiers(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"", strings.Repeat("a", maximumIdentifierBytes+1)} {
		if err := f.s.DeleteRollout(t.Context(), f.actor, id); err != ErrInvalid {
			t.Fatal(id, err)
		}
		if err := f.s.DeleteRelease(t.Context(), f.actor, id); err != ErrInvalid {
			t.Fatal(id, err)
		}
	}
	if err := f.s.DeleteRollout(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := f.s.DeleteRelease(t.Context(), f.actor, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
}
