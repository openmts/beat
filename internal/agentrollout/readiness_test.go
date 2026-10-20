package agentrollout

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestRuntimeReadinessUsesCurrentPinnedTrust(t *testing.T) {
	f := newFixture(t)
	if err := f.s.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_release_trust SET revoked_at=?", f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ready(t.Context()); err == nil {
		t.Fatal("revoked trust advertised readiness")
	}
	if _, err := f.db.DB.Exec("UPDATE agent_release_trust SET revoked_at=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB.Exec("UPDATE agent_release_signature_state SET policy_revision='foreign'"); err == nil {
		t.Fatal("fixture expected foreign-key enforcement")
	}
	if _, err := f.db.DB.Exec("DELETE FROM agent_release_signature_state"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ready(t.Context()); err == nil {
		t.Fatal("missing pinned policy advertised readiness")
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Ready(t.Context()); err == nil {
		t.Fatal("closed database advertised readiness")
	}
}

func TestDisabledReadinessNeedsNoRolloutTables(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	s, err := NewContext(t.Context(), Config{Database: database})
	if err != nil || s.Ready(t.Context()) != nil {
		t.Fatal("disabled readiness accessed rollout tables", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewContext(ctx, Config{Database: database}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled startup accepted", err)
	}
	var absentContext context.Context
	if _, err := NewContext(absentContext, Config{Database: database}); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil startup context accepted", err)
	}
}
