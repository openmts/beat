package store

import (
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestMetadataRejectsCorruptedDomainResults(t *testing.T) {
	cases := map[string]string{
		"group.create":      "AFTER INSERT ON groups BEGIN UPDATE groups SET name='corrupted' WHERE id=NEW.id;END",
		"group.update":      "AFTER UPDATE OF name ON groups BEGIN UPDATE groups SET name='corrupted' WHERE id=NEW.id;END",
		"group.default":     "AFTER INSERT ON admin_audit_events BEGIN UPDATE groups SET is_default=0;END",
		"group.delete":      "AFTER DELETE ON groups BEGIN INSERT INTO groups(id,name,sort_order,is_default,created_at,updated_at) VALUES(OLD.id,OLD.name,OLD.sort_order,0,OLD.created_at,OLD.updated_at);END",
		"group.sort":        "AFTER INSERT ON admin_audit_events BEGIN UPDATE groups SET sort_order=99;END",
		"node.sort":         "AFTER INSERT ON admin_audit_events BEGIN UPDATE nodes SET sort_order=99;END",
		"node.token.rotate": "AFTER UPDATE OF agent_token_hash ON nodes BEGIN UPDATE nodes SET agent_token_prefix='corrupted';END",
	}
	for action, body := range cases {
		t.Run(action, func(t *testing.T) {
			f := newMetadataFixture(t)
			before := f.snapshot(t)
			if _, err := f.db.Exec("CREATE TRIGGER corrupt_domain " + body); err != nil {
				t.Fatal(err)
			}
			if err := f.mutate(t, action); err == nil {
				t.Fatal("corrupted domain result committed")
			}
			if f.snapshot(t) != before {
				t.Fatal("corrupted result survived rollback")
			}
		})
	}
}

func TestMetadataAuthorityQueryFailures(t *testing.T) {
	f := newMetadataFixture(t)
	if _, err := f.db.Exec("ALTER TABLE admin_sessions RENAME TO unavailable_sessions"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "group.create"); err == nil {
		t.Fatal("unavailable authority accepted")
	}
	f.ctx = WithMetadataActor(t.Context(), MetadataActor{Bootstrap: true})
	if _, err := f.db.Exec("ALTER TABLE admin_users RENAME TO unavailable_users"); err != nil {
		t.Fatal(err)
	}
	if err := f.mutate(t, "group.create"); err == nil {
		t.Fatal("unavailable setup status accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.nodes.VerifyAgentIdentity(ctx, model.AgentIdentity{NodeID: f.node.ID}); err == nil {
		t.Fatal("canceled credential check accepted")
	}
	if nodeCredentialBinding(model.Node{}) != model.AgentCredentialLegacy {
		t.Fatal("legacy binding")
	}
}

func TestMetadataAuditAndSnapshotMissingRowsFailClosed(t *testing.T) {
	f := newMetadataFixture(t)
	tx, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(f.ctx, tx, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("DROP TABLE admin_audit_events"); err != nil {
		t.Fatal(err)
	}
	if err := mutation.verifyAudit(model.AdminAuditEvent{}); err == nil {
		t.Fatal("missing audit table accepted")
	}
	if err := verifyNodeSnapshot(t.Context(), tx, model.Node{ID: "missing"}); err == nil {
		t.Fatal("missing response accepted")
	}
	if err := verifyMetadataOrder(t.Context(), tx, metadataOrder{table: "invalid"}); err == nil {
		t.Fatal("untrusted sort table accepted")
	}
	if err := verifyMetadataOrder(t.Context(), tx, metadataOrder{table: "nodes", ids: []string{"missing"}}); err == nil {
		t.Fatal("missing sort resource accepted")
	}
}
