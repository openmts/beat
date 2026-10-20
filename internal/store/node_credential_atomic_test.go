package store

import (
	"testing"
	"time"
)

func TestCredentialResponseReadFailureRollsBackCreation(t *testing.T) {
	database := setupTestDB(t)
	nodes := NewNodeStore(database.DB)
	_, err := database.DB.Exec(`CREATE TRIGGER corrupt_node AFTER INSERT ON nodes BEGIN UPDATE nodes SET port = 'invalid' WHERE id = NEW.id; END`)
	if err != nil {
		t.Fatal(err)
	}
	token := testAgentToken(t, 21)
	node, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "atomic-create", Host: "localhost", Port: 22}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: time.Now()})
	if err == nil || node != nil {
		t.Fatal("unreadable credential response committed")
	}
	var count int
	if err := database.DB.QueryRow("SELECT count(*) FROM nodes WHERE name = ?", "atomic-create").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed create persisted node")
	}
}

func TestCredentialResponseReadFailureRollsBackRotationAndRevocation(t *testing.T) {
	for _, operation := range []string{"rotate", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			database := setupTestDB(t)
			nodes := NewNodeStore(database.DB)
			token := testAgentToken(t, 22)
			node, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "atomic-update", Host: "localhost", Port: 22}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.DB.Exec(`CREATE TRIGGER corrupt_node AFTER UPDATE OF agent_token_hash, agent_token_revoked_at ON nodes BEGIN UPDATE nodes SET port = 'invalid' WHERE id = NEW.id; END`)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "rotate" {
				next := testAgentToken(t, 23)
				_, err = nodes.RotateAgentToken(t.Context(), node.ID, AgentCredential{Hash: next.Hash, Prefix: next.Prefix, CreatedAt: time.Now()})
			} else {
				_, err = nodes.RevokeAgentToken(t.Context(), node.ID, time.Now())
			}
			if err == nil {
				t.Fatal("unreadable response committed")
			}
			authenticated, err := nodes.AuthenticateAgentToken(t.Context(), token.Plaintext)
			if err != nil || authenticated == nil {
				t.Fatal("failed mutation invalidated original credential", err)
			}
		})
	}
}

func TestCredentialCommitFailurePreservesOriginalToken(t *testing.T) {
	database := setupTestDB(t)
	nodes := NewNodeStore(database.DB)
	token := testAgentToken(t, 24)
	node, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "commit-failure", Host: "localhost", Port: 22}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec(`CREATE TRIGGER break_group AFTER UPDATE OF agent_token_revoked_at ON nodes BEGIN UPDATE nodes SET group_id = 'missing-group' WHERE id = NEW.id; END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("PRAGMA defer_foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.RevokeAgentToken(t.Context(), node.ID, time.Now()); err == nil {
		t.Fatal("deferred constraint failure accepted")
	}
	authenticated, err := nodes.AuthenticateAgentToken(t.Context(), token.Plaintext)
	if err != nil || authenticated == nil {
		t.Fatal("commit failure revoked original credential", err)
	}
}

func TestCredentialMutationStatementFailurePreservesOriginalToken(t *testing.T) {
	database := setupTestDB(t)
	nodes := NewNodeStore(database.DB)
	token := testAgentToken(t, 25)
	node, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "statement-failure", Host: "localhost", Port: 22}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.DB.Exec(`CREATE TRIGGER reject_revoke BEFORE UPDATE OF agent_token_revoked_at ON nodes BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.RevokeAgentToken(t.Context(), node.ID, time.Now()); err == nil {
		t.Fatal("statement failure accepted")
	}
	authenticated, err := nodes.AuthenticateAgentToken(t.Context(), token.Plaintext)
	if err != nil || authenticated == nil {
		t.Fatal("statement failure revoked original credential", err)
	}
}
