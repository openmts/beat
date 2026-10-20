package agentrollout

import (
	"testing"
	"time"
)

func TestInitialLeaseIncludesObservationButHonorsWindowAndManifest(t *testing.T) {
	f := newFixture(t)
	ids := f.nodes(t, 3)
	release := f.release(t)
	policy := Policy{ObservationSeconds: 3600}
	first := leaseWithPolicy(t, f, CreateInput{Name: "long observation", ReleaseID: release.ID, NodeIDs: ids[:1], Policy: policy})
	if want := f.now.Add(75 * time.Minute); !first.Equal(want) {
		t.Fatal(want, first)
	}
	policy.WindowEnd = f.now.Add(5 * time.Minute)
	second := leaseWithPolicy(t, f, CreateInput{Name: "short window", ReleaseID: release.ID, NodeIDs: ids[1:2], Policy: policy})
	if !second.Equal(policy.WindowEnd) {
		t.Fatal(policy.WindowEnd, second)
	}
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = "2.1.0"
	manifest.ExpiresAt = f.now.Add(10 * time.Minute)
	f.sign(t, manifest)
	release = f.release(t)
	third := leaseWithPolicy(t, f, CreateInput{Name: "short release", ReleaseID: release.ID, NodeIDs: ids[2:], Policy: Policy{ObservationSeconds: 3600}})
	if !third.Equal(manifest.ExpiresAt) {
		t.Fatal(manifest.ExpiresAt, third)
	}
	if _, err := f.s.Create(t.Context(), f.actor, CreateInput{Name: "too long", ReleaseID: release.ID, NodeIDs: ids, Policy: Policy{ObservationSeconds: 3601}}); err != ErrInvalid {
		t.Fatal(err)
	}
}

func leaseWithPolicy(t *testing.T, f *fixture, input CreateInput) time.Time {
	t.Helper()
	rollout, err := f.s.Create(t.Context(), f.actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.s.Lease(t.Context(), f.identities[input.NodeIDs[0]], capabilities())
	if err != nil || assignment == nil {
		t.Fatal(assignment, err)
	}
	return assignment.ExpiresAt
}
