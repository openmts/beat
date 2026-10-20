package agentrollout

import (
	"sync"
	"testing"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

func TestTrustRevisionBindsCachedReleaseAndFinalAssignment(t *testing.T) {
	for _, query := range []string{"UPDATE agent_release_trust SET revision=revision+1", "UPDATE agent_release_trust SET public_key=zeroblob(32)"} {
		t.Run(query, func(t *testing.T) {
			f := newFixture(t)
			rollout, targets := f.rollout(t, 1)
			if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
				t.Fatal(err)
			}
			f.exec(t, query)
			if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || output != nil {
				t.Fatal("stale cached trust assigned", err)
			}
		})
	}
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	f.exec(t, "CREATE TRIGGER change_trust AFTER INSERT ON agent_rollout_events BEGIN UPDATE agent_release_trust SET revision=revision+1;END")
	if output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities()); err == nil || output != nil {
		t.Fatal("late trust change assigned", err)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollout_attempts", 0)
}

func TestLateTrustChangeRollsBackImportAndProtocolMustMatchServer(t *testing.T) {
	f := newFixture(t)
	f.exec(t, "CREATE TRIGGER change_trust AFTER INSERT ON admin_audit_events BEGIN UPDATE agent_release_trust SET revision=revision+1;END")
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err == nil {
		t.Fatal("trust change after audit committed")
	}
	f.count(t, "SELECT COUNT(*) FROM agent_releases", 0)
	f.exec(t, "DROP TRIGGER change_trust")
	manifest, err := decodeManifest(f.signed.Payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest.MinimumProtocol = f.s.protocol + 1
	manifest.MaximumProtocol = f.s.protocol + 1
	f.sign(t, manifest)
	if _, err := f.s.Import(t.Context(), f.actor, f.signed); err != ErrManifest {
		t.Fatal("server-incompatible protocol imported", err)
	}
}

func TestConcurrentPollsCreateOnlyOneAttempt(t *testing.T) {
	f := newFixture(t)
	rollout, targets := f.rollout(t, 1)
	if err := f.s.Promote(t.Context(), f.actor, rollout.ID); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	outputs := make(chan *protocol.Assignment, 2)
	for range 2 {
		wait.Go(func() {
			output, err := f.s.Lease(t.Context(), f.identities[targets[0].NodeID], capabilities())
			if err != nil && err != ErrConflict {
				t.Error(err)
			}
			outputs <- output
		})
	}
	wait.Wait()
	close(outputs)
	admitted := 0
	for output := range outputs {
		if output != nil {
			admitted++
		}
	}
	if admitted != 1 {
		t.Fatal("concurrent assignments admitted", admitted)
	}
	f.count(t, "SELECT COUNT(*) FROM agent_rollout_attempts", 1)
}
