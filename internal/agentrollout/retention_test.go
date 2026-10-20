package agentrollout

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// terminalRollout 用独立版本的清单建立 rollout 并立即终止它；返回发布与 rollout。
func terminalRollout(t *testing.T, f *fixture, version, node string) (Release, Rollout) {
	t.Helper()
	release := retentionRelease(t, f, version)
	rollout, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "Retention " + version, ReleaseID: release.ID, NodeIDs: []string{node}, Policy: Policy{CanaryCount: 1, WaveSize: 1, ObservationSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Abort(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	return release, rollout
}

// runningRollout 建立 rollout 但不终止它，用于验证清理不会触碰未完成对象。
func runningRollout(t *testing.T, f *fixture, version, node string) (Release, Rollout) {
	t.Helper()
	release := retentionRelease(t, f, version)
	rollout, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "Running " + version, ReleaseID: release.ID, NodeIDs: []string{node}, Policy: Policy{CanaryCount: 1, WaveSize: 1, ObservationSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	return release, rollout
}

func retentionRelease(t *testing.T, f *fixture, version string) Release {
	t.Helper()
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = version
	f.sign(t, manifest)
	return f.release(t)
}

func countRows(t *testing.T, f *fixture, query string, args ...any) int {
	t.Helper()
	var count int
	if err := f.db.DB.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestRetentionPolicyBoundsAndEnablement(t *testing.T) {
	for name, policy := range map[string]RetentionPolicy{
		"zero":    {},
		"maximum": {RolloutDays: maximumRetentionDays, ReleaseDays: maximumRetentionDays},
	} {
		if !policy.Valid() {
			t.Fatal("valid policy rejected", name)
		}
	}
	for name, policy := range map[string]RetentionPolicy{
		"negative rollouts": {RolloutDays: -1},
		"oversized release": {ReleaseDays: maximumRetentionDays + 1},
	} {
		if policy.Valid() {
			t.Fatal("invalid policy accepted", name)
		}
	}
	if (RetentionPolicy{}).Enabled() {
		t.Fatal("zero policy reported as enabled")
	}
	if !(RetentionPolicy{RolloutDays: 1}).Enabled() {
		t.Fatal("positive policy reported as disabled")
	}
}

func TestPruneWithoutPolicyKeepsHistoryAndWritesNoAudit(t *testing.T) {
	f := newFixture(t)
	nodes := f.nodes(t, 1)
	_, rollout := terminalRollout(t, f, "2.0.0", nodes[0])
	f.now = f.now.AddDate(0, 0, 400)
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{})
	if err != nil || counts != (RetentionCounts{}) {
		t.Fatal(counts, err)
	}
	if kept := countRows(t, f, "SELECT COUNT(*) FROM agent_rollouts WHERE id=?", rollout.ID); kept != 1 {
		t.Fatal("zero policy removed a rollout", kept)
	}
	if audits := countRows(t, f, "SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.delete'"); audits != 0 {
		t.Fatal("zero policy wrote audit rows", audits)
	}
}

func TestPruneRejectsInvalidPolicyAndDisabledService(t *testing.T) {
	f := newFixture(t)
	for _, policy := range []RetentionPolicy{{RolloutDays: -1}, {ReleaseDays: maximumRetentionDays + 1}} {
		if _, err := f.s.Prune(t.Context(), policy); !errors.Is(err, ErrInvalid) {
			t.Fatal(policy, err)
		}
	}
	f.s.enabled = false
	if _, err := f.s.Prune(t.Context(), RetentionPolicy{RolloutDays: 1}); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
}

func TestPruneDeletesOnlyExpiredTerminalRollouts(t *testing.T) {
	f := newFixture(t)
	nodes := f.nodes(t, 3)
	_, expired := terminalRollout(t, f, "2.1.0", nodes[0])
	_, inflight := runningRollout(t, f, "2.2.0", nodes[1])
	_, recent := terminalRollout(t, f, "2.3.0", nodes[2])
	f.now = f.now.AddDate(0, 0, 100)
	// recent 代表保留期内的终态 rollout：把它的更新时刻挪到清理窗口以内。
	if _, err := f.db.DB.Exec("UPDATE agent_rollouts SET updated_at=? WHERE id=?", f.now.AddDate(0, 0, -20), recent.ID); err != nil {
		t.Fatal(err)
	}
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{RolloutDays: 30})
	if err != nil || counts.Rollouts != 1 {
		t.Fatal(counts, err)
	}
	for _, test := range []struct {
		rollout Rollout
		want    int
	}{{expired, 0}, {inflight, 1}, {recent, 1}} {
		kept := countRows(t, f, "SELECT COUNT(*) FROM agent_rollouts WHERE id=?", test.rollout.ID)
		if kept != test.want {
			t.Fatal("unexpected rollout state", test.rollout.ID, kept)
		}
	}
	for query, expected := range map[string]int{
		"SELECT COUNT(*) FROM agent_rollout_targets WHERE rollout_id=?":                                                            0,
		"SELECT COUNT(*) FROM agent_rollout_attempts WHERE target_id IN (SELECT id FROM agent_rollout_targets WHERE rollout_id=?)": 0,
		"SELECT COUNT(*) FROM agent_rollout_events WHERE rollout_id=?":                                                             0,
		"SELECT COUNT(*) FROM admin_audit_events WHERE action='rollout.delete' AND resource_id=? AND actor_username='system'":      1,
	} {
		if count := countRows(t, f, query, expired.ID); count != expected {
			t.Fatal(query, count)
		}
	}
}

func TestPruneDeletesRevokedReleasesAndArtifactFiles(t *testing.T) {
	f := newFixture(t)
	release := f.release(t)
	if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.AddDate(0, 0, 100)
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{ReleaseDays: 30})
	if err != nil || counts.Releases != 1 || counts.Artifacts != 1 {
		t.Fatal(counts, err)
	}
	if kept := countRows(t, f, "SELECT COUNT(*) FROM agent_releases WHERE id=?", release.ID); kept != 0 {
		t.Fatal("expired revoked release survived", kept)
	}
	digest := sha256.Sum256(f.artifact)
	if _, err := os.Stat(filepath.Join(f.s.directory, hex.EncodeToString(digest[:])+artifactFileSuffix)); !os.IsNotExist(err) {
		t.Fatal("orphan artifact file survived retention", err)
	}
	audits := countRows(t, f, "SELECT COUNT(*) FROM admin_audit_events WHERE action='release.delete' AND resource_id=? AND actor_username='system' AND detail_json LIKE '%\"reason\":\"retention\"%'", release.ID)
	if audits != 1 {
		t.Fatal("retention audit row missing", audits)
	}
}

func TestPruneKeepsReferencedAndUnrevokedReleases(t *testing.T) {
	f := newFixture(t)
	nodes := f.nodes(t, 1)
	_, rollout := runningRollout(t, f, "2.4.0", nodes[0])
	referenced, err := f.s.ReleaseDetail(t.Context(), f.actor, rollout.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevokeRelease(t.Context(), f.actor, referenced.ID); err != nil {
		t.Fatal(err)
	}
	unrevoked := retentionRelease(t, f, "2.5.0")
	f.now = f.now.AddDate(0, 0, 100)
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{ReleaseDays: 30})
	if err != nil || counts.Releases != 0 {
		t.Fatal(counts, err)
	}
	for _, kept := range []string{referenced.ID, unrevoked.ID} {
		if remaining := countRows(t, f, "SELECT COUNT(*) FROM agent_releases WHERE id=?", kept); remaining != 1 {
			t.Fatal("release inside the retention rules disappeared", kept, remaining)
		}
	}
	if remaining := countRows(t, f, "SELECT COUNT(*) FROM agent_rollouts WHERE id=?", rollout.ID); remaining != 1 {
		t.Fatal("running rollout disappeared", remaining)
	}
}

func TestPruneRemovesExpiredRolloutAndItsReleaseInOnePass(t *testing.T) {
	f := newFixture(t)
	nodes := f.nodes(t, 1)
	release, rollout := terminalRollout(t, f, "2.6.0", nodes[0])
	if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.AddDate(0, 0, 60)
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{RolloutDays: 30, ReleaseDays: 30})
	if err != nil || counts.Rollouts != 1 || counts.Releases != 1 || counts.Artifacts != 1 {
		t.Fatal(counts, err)
	}
	if remaining := countRows(t, f, "SELECT COUNT(*) FROM agent_rollouts WHERE id=?", rollout.ID); remaining != 0 {
		t.Fatal("rollout survived", remaining)
	}
	if remaining := countRows(t, f, "SELECT COUNT(*) FROM agent_releases WHERE id=?", release.ID); remaining != 0 {
		t.Fatal("release survived", remaining)
	}
}

func TestPruneDeletesEveryExpiredBatch(t *testing.T) {
	f := newFixture(t)
	expired := retentionBatchSize + 1
	nodes := f.nodes(t, expired)
	for index, node := range nodes {
		terminalRollout(t, f, fmt.Sprintf("4.%d.0", index), node)
	}
	f.now = f.now.AddDate(0, 0, 60)
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{RolloutDays: 30})
	if err != nil || counts.Rollouts != expired {
		t.Fatal(counts, err)
	}
	if remaining := countRows(t, f, "SELECT COUNT(*) FROM agent_rollouts"); remaining != 0 {
		t.Fatal("batch pruning stopped early", remaining)
	}
}

func TestPruneKeepsSharedArtifactUntilLastReleaseIsGone(t *testing.T) {
	f := newFixture(t)
	first := f.release(t)
	second := retentionRelease(t, f, "5.0.0")
	for _, release := range []Release{first, second} {
		if err := f.s.RevokeRelease(t.Context(), f.actor, release.ID); err != nil {
			t.Fatal(err)
		}
	}
	f.now = f.now.AddDate(0, 0, 100)
	counts, err := f.s.Prune(t.Context(), RetentionPolicy{ReleaseDays: 30})
	if err != nil || counts.Releases != 2 || counts.Artifacts != 2 {
		t.Fatal(counts, err)
	}
	digest := sha256.Sum256(f.artifact)
	if _, err := os.Stat(filepath.Join(f.s.directory, hex.EncodeToString(digest[:])+artifactFileSuffix)); !os.IsNotExist(err) {
		t.Fatal("shared artifact file survived the last reference", err)
	}
}
