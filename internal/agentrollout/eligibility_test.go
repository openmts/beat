package agentrollout

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestAssignmentsExposeBoundedHeldReasons(t *testing.T) {
	cases := map[string]struct {
		change func(*protocol.Capabilities)
		reason string
	}{
		"container": {func(c *protocol.Capabilities) { c.Manager = "external_manager" }, "external_manager"},
		"disabled":  {func(c *protocol.Capabilities) { c.UpdateEnabled = false }, "manual_only"},
		"manager":   {func(c *protocol.Capabilities) { c.Manager = "unknown" }, "manual_only"},
		"space":     {func(c *protocol.Capabilities) { c.AvailableBytes = 0 }, "insufficient_space"},
		"protocol":  {func(c *protocol.Capabilities) { c.ProtocolVersion = 99 }, "protocol_incompatible"},
		"version":   {func(c *protocol.Capabilities) { c.CurrentVersion = "changed" }, "version_changed"},
		"unknown":   {func(c *protocol.Capabilities) { c.CurrentVersion = "" }, "unknown_version"},
		"platform":  {func(c *protocol.Capabilities) { c.Arch = "arm64" }, "unsupported_platform"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			caps := capabilities()
			test.change(&caps)
			for range 2 {
				assignment, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], caps)
				if err != nil || assignment != nil {
					t.Fatal("held node assigned", err)
				}
			}
			_, targets, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
			if err != nil || targets[0].Reason != test.reason {
				t.Fatal(targets, err)
			}
			var count int
			if err := f.db.DB.QueryRow("SELECT COUNT(*) FROM agent_rollout_events WHERE kind='assignment.held'").Scan(&count); err != nil || count != 1 {
				t.Fatal("polling duplicated held events", count, err)
			}
		})
	}
}

func TestWaveCapacityMissingArtifactsAndFreshness(t *testing.T) {
	f := newFixture(t)
	ids := f.nodes(t, 2)
	release := f.release(t)
	rollout, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "Capacity", ReleaseID: release.ID, NodeIDs: ids, Policy: Policy{CanaryCount: 2, MaxConcurrent: 1, ObservationSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	if assignment, err := f.s.Lease(t.Context(), f.identities[ids[0]], capabilities()); err != nil || assignment == nil {
		t.Fatal(err)
	}
	if assignment, err := f.s.Lease(t.Context(), f.identities[ids[1]], capabilities()); err != nil || assignment != nil {
		t.Fatal("capacity ignored", err)
	}
	f.now = f.now.Add(2 * time.Minute)
	if assignment, err := f.s.Lease(t.Context(), f.identities[ids[1]], capabilities()); err != nil || assignment != nil {
		t.Fatal("stale heartbeat assigned", err)
	}
}

func TestSelectionSnapshotsTagsAndGroupsAndRejectsUnknownIDs(t *testing.T) {
	f := newFixture(t)
	ids := f.nodes(t, 2)
	release := f.release(t)
	rollout, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "Tags", ReleaseID: release.ID, Tags: []string{"canary"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE nodes SET tags='[]'"); err != nil {
		t.Fatal(err)
	}
	_, targets, err := f.s.Detail(t.Context(), f.actor, rollout.ID)
	if err != nil || len(targets) != 2 {
		t.Fatal("target snapshot changed with tags", targets, err)
	}
	for _, input := range []CreateInput{{Name: "missing", ReleaseID: release.ID, NodeIDs: []string{"missing"}}, {Name: "duplicate", ReleaseID: release.ID, NodeIDs: []string{ids[0], ids[0]}}, {Name: "empty", ReleaseID: release.ID}, {Name: "missing-group", ReleaseID: release.ID, GroupIDs: []string{"missing"}}} {
		if _, err := f.s.Create(t.Context(), f.actor, input); err == nil {
			t.Fatal("invalid target selection accepted")
		}
	}
	var group string
	if err := f.db.DB.QueryRow("SELECT group_id FROM nodes WHERE id=?", ids[0]).Scan(&group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "Group", ReleaseID: release.ID, GroupIDs: []string{group}}); err != nil {
		t.Fatal(err)
	}
}

func TestVersionOrderingRejectsUnknownAndDowngrade(t *testing.T) {
	for _, test := range []struct {
		target, current string
		order           int
		valid           bool
	}{{"2.0.0", "1.0.0", 1, true}, {"v1.0.0", "2.0.0", -1, true}, {"1.0.0", "1.0.0", 0, true}, {"dev", "1.0.0", 0, false}, {"1.0.x", "1.0.0", 0, false}} {
		order, valid := compareVersion(test.target, test.current)
		if order != test.order || valid != test.valid {
			t.Fatal(test, order, valid)
		}
	}
}
