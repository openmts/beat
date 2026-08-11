package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/model"
)

func TestNodeCredentialLifecycle(t *testing.T) {
	s := setupTestDB(t)
	nodes := NewNodeStore(s.DB)
	ctx := context.Background()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	first := testAgentToken(t, 1)

	node, err := nodes.CreateManagedNode(ctx, ManagedNodeInput{
		Name: "managed-node", Host: "127.0.0.1", Port: 22,
	}, AgentCredential{Hash: first.Hash, Prefix: first.Prefix, CreatedAt: now})
	if err != nil {
		t.Fatalf("create managed node: %v", err)
	}
	if node.AgentCredentialStatus() != model.AgentCredentialActive {
		t.Fatalf("credential status = %q, want active", node.AgentCredentialStatus())
	}

	authenticated, err := nodes.AuthenticateAgentToken(ctx, first.Plaintext)
	if err != nil || authenticated == nil || authenticated.ID != node.ID {
		t.Fatalf("authenticate first token: node=%#v err=%v", authenticated, err)
	}
	if leaked := countStoredText(t, s, first.Plaintext); leaked != 0 {
		t.Fatalf("plaintext token stored in SQLite %d times", leaked)
	}

	second := testAgentToken(t, 2)
	rotated, err := nodes.RotateAgentToken(ctx, node.ID, AgentCredential{
		Hash: second.Hash, Prefix: second.Prefix, CreatedAt: now.Add(time.Minute),
	})
	if err != nil || rotated == nil {
		t.Fatalf("rotate token: node=%#v err=%v", rotated, err)
	}
	old, err := nodes.AuthenticateAgentToken(ctx, first.Plaintext)
	if err != nil || old != nil {
		t.Fatalf("old token authentication = %#v, err=%v; want nil", old, err)
	}
	current, err := nodes.AuthenticateAgentToken(ctx, second.Plaintext)
	if err != nil || current == nil || current.ID != node.ID {
		t.Fatalf("new token authentication = %#v, err=%v", current, err)
	}

	revoked, err := nodes.RevokeAgentToken(ctx, node.ID, now.Add(2*time.Minute))
	if err != nil || revoked == nil || revoked.AgentCredentialStatus() != model.AgentCredentialRevoked {
		t.Fatalf("revoke token: node=%#v err=%v", revoked, err)
	}
	current, err = nodes.AuthenticateAgentToken(ctx, second.Plaintext)
	if err != nil || current != nil {
		t.Fatalf("revoked token authentication = %#v, err=%v; want nil", current, err)
	}
}

func TestManagedNodeNameIsUnique(t *testing.T) {
	s := setupTestDB(t)
	nodes := NewNodeStore(s.DB)
	ctx := context.Background()
	credential := testAgentToken(t, 3)
	input := ManagedNodeInput{Name: "unique-node", Host: "localhost", Port: 22}
	value := AgentCredential{Hash: credential.Hash, Prefix: credential.Prefix, CreatedAt: model.NowUTC()}
	if _, err := nodes.CreateManagedNode(ctx, input, value); err != nil {
		t.Fatalf("create first node: %v", err)
	}
	second := testAgentToken(t, 6)
	value = AgentCredential{Hash: second.Hash, Prefix: second.Prefix, CreatedAt: model.NowUTC()}
	if _, err := nodes.CreateManagedNode(ctx, input, value); !errors.Is(err, ErrNodeNameConflict) {
		t.Fatalf("duplicate error = %v, want ErrNodeNameConflict", err)
	}
}

func TestLegacyNodeAuthenticationExcludesManagedNodes(t *testing.T) {
	s := setupTestDB(t)
	nodes := NewNodeStore(s.DB)
	ctx := context.Background()
	legacy, err := nodes.UpsertNode(ctx, "legacy-node", "127.0.0.1", 22)
	if err != nil {
		t.Fatalf("create legacy node: %v", err)
	}
	got, err := nodes.AuthenticateLegacyNode(ctx, legacy.Name)
	if err != nil || got == nil || got.ID != legacy.ID {
		t.Fatalf("authenticate legacy node: node=%#v err=%v", got, err)
	}
	managedToken := testAgentToken(t, 4)
	if _, err := nodes.RotateAgentToken(ctx, legacy.ID, AgentCredential{
		Hash: managedToken.Hash, Prefix: managedToken.Prefix, CreatedAt: model.NowUTC(),
	}); err != nil {
		t.Fatalf("activate legacy node: %v", err)
	}
	got, err = nodes.AuthenticateLegacyNode(ctx, legacy.Name)
	if err != nil || got != nil {
		t.Fatalf("managed node authenticated as legacy: node=%#v err=%v", got, err)
	}
}

func TestTouchAgentTokenIsThrottled(t *testing.T) {
	s := setupTestDB(t)
	nodes := NewNodeStore(s.DB)
	ctx := context.Background()
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	token := testAgentToken(t, 5)
	node, err := nodes.CreateManagedNode(ctx, ManagedNodeInput{
		Name: "touch-node", Host: "localhost", Port: 22,
	}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: now})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	for _, usedAt := range []time.Time{now, now.Add(time.Minute), now.Add(6 * time.Minute)} {
		if err := nodes.TouchAgentToken(ctx, node.ID, usedAt, 5*time.Minute); err != nil {
			t.Fatalf("touch token: %v", err)
		}
	}
	got, err := nodes.GetNode(ctx, node.ID)
	if err != nil || got == nil || got.AgentTokenLastUsedAt == nil {
		t.Fatalf("get touched node: node=%#v err=%v", got, err)
	}
	if !got.AgentTokenLastUsedAt.Equal(now.Add(6 * time.Minute)) {
		t.Fatalf("last used = %v, want %v", got.AgentTokenLastUsedAt, now.Add(6*time.Minute))
	}
}

func TestNodeCredentialErrorsAndMissingNodes(t *testing.T) {
	s := setupTestDB(t)
	nodes := NewNodeStore(s.DB)
	ctx := context.Background()
	token := testAgentToken(t, 7)
	first, err := nodes.CreateManagedNode(ctx, ManagedNodeInput{
		Name: "first-token", Host: "localhost", Port: 22,
	}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: model.NowUTC()})
	if err != nil {
		t.Fatalf("create first token node: %v", err)
	}
	other, err := nodes.UpsertNode(ctx, "other-token", "localhost", 22)
	if err != nil {
		t.Fatalf("create other node: %v", err)
	}
	if _, err := nodes.RotateAgentToken(ctx, other.ID, AgentCredential{
		Hash: token.Hash, Prefix: token.Prefix, CreatedAt: model.NowUTC(),
	}); !errors.Is(err, ErrAgentTokenConflict) {
		t.Fatalf("token conflict error = %v", err)
	}
	if got, err := nodes.RotateAgentToken(ctx, "missing", AgentCredential{
		Hash: testAgentToken(t, 8).Hash, Prefix: "prefix", CreatedAt: model.NowUTC(),
	}); err != nil || got != nil {
		t.Fatalf("rotate missing = %#v, err=%v", got, err)
	}
	if got, err := nodes.RevokeAgentToken(ctx, "missing", model.NowUTC()); err != nil || got != nil {
		t.Fatalf("revoke missing = %#v, err=%v", got, err)
	}
	if got, err := nodes.AuthenticateAgentToken(ctx, "invalid"); err != nil || got != nil {
		t.Fatalf("malformed authentication = %#v, err=%v", got, err)
	}
	if got, err := nodes.UpdateNodeHeartbeat(ctx, "missing", NodeHeartbeat{}); err != nil || got != nil {
		t.Fatalf("missing heartbeat = %#v, err=%v", got, err)
	}
	if first.ID == "" {
		t.Fatal("first node ID is empty")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if _, err := nodes.CreateManagedNode(ctx, ManagedNodeInput{
		Name: "closed", Host: "localhost", Port: 22,
	}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: model.NowUTC()}); err == nil {
		t.Fatal("expected create database error")
	}
	if _, err := nodes.RotateAgentToken(ctx, first.ID, AgentCredential{
		Hash: token.Hash, Prefix: token.Prefix, CreatedAt: model.NowUTC(),
	}); err == nil {
		t.Fatal("expected rotate database error")
	}
	if _, err := nodes.RevokeAgentToken(ctx, first.ID, model.NowUTC()); err == nil {
		t.Fatal("expected revoke database error")
	}
	if _, err := nodes.AuthenticateAgentToken(ctx, token.Plaintext); err == nil {
		t.Fatal("expected token database error")
	}
	if _, err := nodes.AuthenticateLegacyNode(ctx, "legacy"); err == nil {
		t.Fatal("expected legacy database error")
	}
	if err := nodes.TouchAgentToken(ctx, first.ID, model.NowUTC(), time.Minute); err == nil {
		t.Fatal("expected touch database error")
	}
	if _, err := nodes.UpdateNodeHeartbeat(ctx, first.ID, NodeHeartbeat{}); err == nil {
		t.Fatal("expected heartbeat database error")
	}
}

func TestCreateManagedNodeRequiresDefaultGroup(t *testing.T) {
	s := setupTestDB(t)
	nodes := NewNodeStore(s.DB)
	if _, err := s.DB.Exec("DELETE FROM groups"); err != nil {
		t.Fatalf("delete groups: %v", err)
	}
	token := testAgentToken(t, 9)
	_, err := nodes.CreateManagedNode(t.Context(), ManagedNodeInput{
		Name: "no-group", Host: "localhost", Port: 22,
	}, AgentCredential{Hash: token.Hash, Prefix: token.Prefix, CreatedAt: model.NowUTC()})
	if err == nil {
		t.Fatal("expected missing default group error")
	}
}

func testAgentToken(t *testing.T, fill byte) agentcredential.Token {
	t.Helper()
	token, err := agentcredential.GenerateFrom(&repeatingReader{value: fill})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

type repeatingReader struct{ value byte }

func (r *repeatingReader) Read(output []byte) (int, error) {
	for index := range output {
		output[index] = r.value
	}
	return len(output), nil
}

func countStoredText(t *testing.T, s *SQLiteStore, value string) int {
	t.Helper()
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM nodes WHERE
		CAST(agent_token_hash AS TEXT) = ? OR agent_token_prefix = ?`, value, value).Scan(&count); err != nil {
		t.Fatalf("search plaintext token: %v", err)
	}
	return count
}
