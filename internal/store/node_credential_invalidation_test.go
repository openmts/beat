package store

import (
	"bytes"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestCredentialInvalidatorRunsOnlyAfterCommitForPreviousEpoch(t *testing.T) {
	f := newMetadataFixture(t)
	old := model.AgentIdentity{NodeID: f.node.ID, Mode: model.AgentCredentialActive, CredentialFingerprint: model.AgentCredentialFingerprint(f.node.AgentTokenHash)}
	if err := f.nodes.VerifyAgentIdentity(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.nodes.WithCredentialInvalidator(func(id, binding string) {
		calls++
		if id != f.node.ID || binding != nodeCredentialBinding(*f.node) {
			t.Error("wrong credential invalidated", id, binding)
		}
		current, err := f.nodes.GetNode(t.Context(), id)
		if err != nil || bytes.Equal(current.AgentTokenHash, f.node.AgentTokenHash) {
			t.Error("callback ran before committed credential", err)
		}
	})
	if _, err := f.db.Exec(`CREATE TRIGGER fail_rotation BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'full');END`); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "node.token.rotate"); err == nil {
		t.Fatal("unaudited rotation succeeded")
	}
	if calls != 0 {
		t.Fatal("failed rotation disconnected connection")
	}
	if _, err := f.db.Exec("DROP TRIGGER fail_rotation"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "node.token.rotate"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("committed rotation did not invalidate old epoch")
	}
	if err := f.nodes.VerifyAgentIdentity(t.Context(), old); err == nil {
		t.Fatal("old credential remained valid")
	}
}

func TestNodeCredentialAndResponseTriggersCannotSilentlyChangeResult(t *testing.T) {
	for _, operation := range []string{"node.token.rotate", "node.token.revoke", "node.update", "site_settings.update"} {
		t.Run(operation, func(t *testing.T) {
			f := newMetadataFixture(t)
			table := "nodes"
			if operation == "site_settings.update" {
				table = "site_settings"
			}
			if _, err := f.db.Exec("CREATE TRIGGER skip_write BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(IGNORE);END"); err != nil {
				t.Fatal(err)
			}
			if err := f.mutate(t, operation); err == nil {
				t.Fatal("ignored mutation succeeded")
			}
		})
	}
	f := newMetadataFixture(t)
	if _, err := f.db.Exec(`CREATE TRIGGER change_response AFTER INSERT ON admin_audit_events BEGIN UPDATE nodes SET alias='changed-after-response';END`); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot(t)
	if err := f.mutate(t, "node.token.rotate"); err == nil {
		t.Fatal("changed response snapshot committed")
	}
	if before != f.snapshot(t) {
		t.Fatal("response mismatch was not rolled back")
	}
}
