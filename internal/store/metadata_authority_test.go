package store

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMetadataAuthorityRevocationAndRecentProof(t *testing.T) {
	for _, action := range metadataActions {
		t.Run(action, func(t *testing.T) {
			f := newMetadataFixture(t)
			if _, err := f.db.Exec("UPDATE admin_sessions SET revoked_at=CURRENT_TIMESTAMP"); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			if err := f.mutate(t, action); !errors.Is(err, ErrMetadataForbidden) {
				t.Fatal(err)
			}
			if before != f.snapshot(t) {
				t.Fatal("revoked actor changed metadata")
			}
		})
	}
	for _, action := range []string{"node.create", "node.token.rotate", "node.token.revoke"} {
		t.Run("recent/"+action, func(t *testing.T) {
			f := newMetadataFixture(t)
			if _, err := f.db.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
				t.Fatal(err)
			}
			if err := f.mutate(t, action); !errors.Is(err, ErrMetadataRecentAuth) {
				t.Fatal(err)
			}
		})
	}
}

func TestMetadataBootstrapScopeAndLateRevocation(t *testing.T) {
	f := newMetadataFixture(t)
	f.ctx = WithMetadataActor(t.Context(), MetadataActor{Bootstrap: true})
	if err := f.mutate(t, "group.create"); !errors.Is(err, ErrMetadataForbidden) {
		t.Fatal("bootstrap survived setup", err)
	}
	if _, err := f.db.Exec("DELETE FROM admin_sessions;DELETE FROM admin_users"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "group.create"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "node.create"); !errors.Is(err, ErrMetadataForbidden) {
		t.Fatal("bootstrap issued credential", err)
	}
	var actor string
	if err := f.db.QueryRow("SELECT actor_username FROM admin_audit_events").Scan(&actor); err != nil || actor != "bootstrap_authority" {
		t.Fatal(actor, err)
	}
}

func TestMetadataAuditIgnoreDeleteAndLateAuthChangeRollback(t *testing.T) {
	for _, body := range []string{
		"BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(IGNORE);END",
		"AFTER INSERT ON admin_audit_events BEGIN DELETE FROM admin_audit_events WHERE id=NEW.id;END",
		"AFTER INSERT ON admin_audit_events BEGIN UPDATE admin_users SET enabled=0;END",
	} {
		t.Run(body, func(t *testing.T) {
			f := newMetadataFixture(t)
			if _, err := f.db.Exec("CREATE TRIGGER corrupt_metadata " + body); err != nil {
				t.Fatal(err)
			}
			before := f.snapshot(t)
			if err := f.mutate(t, "group.create"); err == nil {
				t.Fatal("corrupt audit committed")
			}
			if before != f.snapshot(t) {
				t.Fatal("corrupt audit left mutation")
			}
		})
	}
}

func TestMetadataOrdinaryManagementDoesNotRequireRecentAuth(t *testing.T) {
	f := newMetadataFixture(t)
	if _, err := f.db.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "node.update"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "group.update"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "site_settings.update"); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(f.ctx)
	cancel()
	f.ctx = canceled
	if err := f.mutate(t, "group.create"); err == nil {
		t.Fatal("canceled mutation succeeded")
	}
	if err := f.nodes.VerifyAgentIdentity(t.Context(), model.AgentIdentity{NodeID: "missing"}); err == nil {
		t.Fatal("missing agent admitted")
	}
}

func TestNodePublicKeyChangesRequireRecentProofButAliasDoesNot(t *testing.T) {
	f := newMetadataFixture(t)
	if _, err := f.db.Exec("UPDATE admin_sessions SET reauthenticated_until=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.UpdateNode(f.ctx, f.node.ID, NodeUpdate{Alias: "ordinary", GroupID: f.group.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.UpdateNode(f.ctx, f.node.ID, NodeUpdate{Alias: "ordinary", GroupID: f.group.ID, SSHPublicKey: "ssh-ed25519 sensitive"}); !errors.Is(err, ErrMetadataRecentAuth) {
		t.Fatal("SSH assignment bypassed recent auth", err)
	}
	node, err := f.nodes.GetNode(t.Context(), f.node.ID)
	if err != nil || node.SSHPublicKey != "" {
		t.Fatal("failed proof modified SSH key", err)
	}
}
