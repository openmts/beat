package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func agentContactFixture(t *testing.T) (*SQLiteStore, *NodeStore, AgentContact) {
	t.Helper()
	sqlite := setupTestDB(t)
	nodes := NewNodeStore(sqlite.DB)
	node, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{Name: "contact-node", Host: "192.0.2.1", Port: 22}, AgentCredential{Hash: []byte(strings.Repeat("a", 32)), Prefix: "prefix", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return sqlite, nodes, AgentContact{Identity: model.AgentIdentity{NodeID: node.ID, NodeName: node.Name, Mode: model.AgentCredentialActive, CredentialFingerprint: model.AgentCredentialFingerprint(node.AgentTokenHash)},
		Heartbeat: NodeHeartbeat{Host: "192.0.2.2", Port: 2222, System: model.SystemInfo{CPUModel: "cpu", OS: "linux", Platform: "test", OSVersion: "v1", Kernel: "kernel", Arch: "arm64", Virtualization: "vm", AgentVersion: "agent"}}}
}

func TestAgentContactAtomicallyStoresInventoryAndThrottlesCredentialTouch(t *testing.T) {
	_, nodes, contact := agentContactFixture(t)
	accepted, err := nodes.AcceptAgentContact(t.Context(), contact)
	if err != nil || accepted.Status != model.NodeStatusOnline || accepted.Host != contact.Heartbeat.Host {
		t.Fatal("contact did not commit", err)
	}
	stored, err := nodes.GetNode(t.Context(), contact.Identity.NodeID)
	if err != nil || stored.AgentTokenLastUsedAt == nil {
		t.Fatal("authenticated token use was not committed", err)
	}
	usedAt := *stored.AgentTokenLastUsedAt
	contact.PreserveInventory = true
	contact.Heartbeat = NodeHeartbeat{Host: "older-host", Port: 1}
	if _, err := nodes.AcceptAgentContact(t.Context(), contact); err != nil {
		t.Fatal(err)
	}
	stored, err = nodes.GetNode(t.Context(), contact.Identity.NodeID)
	if err != nil || stored.Host != "192.0.2.2" || stored.CPUModel != "cpu" || stored.OS != "linux" || stored.AgentVersion != "agent" || !usedAt.Equal(*stored.AgentTokenLastUsedAt) {
		t.Fatal("duplicate rewound inventory or touched token too often", err)
	}
}

func TestAgentContactRejectsRevokedRotatedAndMissingIdentity(t *testing.T) {
	for _, scenario := range []string{"missing", "fingerprint", "mode", "revoked", "rotated"} {
		t.Run(scenario, func(t *testing.T) {
			_, nodes, contact := agentContactFixture(t)
			switch scenario {
			case "missing":
				contact.Identity.NodeID = "missing"
			case "fingerprint":
				contact.Identity.CredentialFingerprint = "invalid"
			case "mode":
				contact.Identity.Mode = model.AgentCredentialLegacy
			case "revoked":
				if _, err := nodes.RevokeAgentToken(t.Context(), contact.Identity.NodeID, time.Now()); err != nil {
					t.Fatal(err)
				}
			case "rotated":
				if _, err := nodes.RotateAgentToken(t.Context(), contact.Identity.NodeID, AgentCredential{Hash: []byte(strings.Repeat("b", 32)), Prefix: "rotated", CreatedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := nodes.AcceptAgentContact(t.Context(), contact); !errors.Is(err, ErrAgentCredentialRejected) {
				t.Fatal("stale identity wrote contact", err)
			}
		})
	}
	_, nodes, contact := agentContactFixture(t)
	legacy, err := nodes.UpsertNode(t.Context(), "legacy", "192.0.2.3", 22)
	if err != nil {
		t.Fatal(err)
	}
	contact.Identity = model.AgentIdentity{NodeID: legacy.ID, Mode: model.AgentCredentialLegacy, CredentialFingerprint: strings.Repeat("c", 64)}
	if _, err := nodes.AcceptAgentContact(t.Context(), contact); err != nil {
		t.Fatal("authenticated legacy credential rejected", err)
	}
}

func TestAgentContactRollsBackHeartbeatWhenTouchOrCommitFails(t *testing.T) {
	for _, stage := range []string{"heartbeat", "touch", "commit", "read"} {
		t.Run(stage, func(t *testing.T) {
			sqlite, nodes, contact := agentContactFixture(t)
			setup := map[string]string{
				"heartbeat": `CREATE TRIGGER fail_contact BEFORE UPDATE OF host ON nodes BEGIN SELECT RAISE(ABORT,'heartbeat'); END`,
				"touch":     `CREATE TRIGGER fail_contact BEFORE UPDATE OF agent_token_last_used_at ON nodes BEGIN SELECT RAISE(ABORT,'touch'); END`,
				"commit":    `CREATE TABLE contact_commit_failure(id TEXT REFERENCES nodes(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER fail_contact AFTER UPDATE OF host ON nodes BEGIN INSERT INTO contact_commit_failure VALUES('missing'); END`,
				"read":      `ALTER TABLE nodes RENAME COLUMN host TO inaccessible_host`,
			}
			if _, err := sqlite.DB.Exec(setup[stage]); err != nil {
				t.Fatal(err)
			}
			if _, err := nodes.AcceptAgentContact(t.Context(), contact); err == nil || errors.Is(err, ErrAgentCredentialRejected) {
				t.Fatal("storage failure hidden as success/unauthorized", err)
			}
			if stage == "read" {
				return
			}
			after, err := nodes.GetNode(t.Context(), contact.Identity.NodeID)
			if err != nil || after.Host != "192.0.2.1" || !after.LastSeen.IsZero() || after.AgentTokenLastUsedAt != nil {
				t.Fatal("failed contact transaction partially changed state", err)
			}
		})
	}
	sqlite, nodes, contact := agentContactFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := nodes.AcceptAgentContact(ctx, contact); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled contact started", err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.AcceptAgentContact(t.Context(), contact); err == nil {
		t.Fatal("closed contact store returned success")
	}
}
