package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentjoin"
)

func TestEnrollmentRejectsInvalidExpiredRevokedAndPlaintext(t *testing.T) {
	sqlite, nodes, token, input := enrollmentFixture(t)
	invalid := input
	invalid.Request.SSHPort = 0
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), invalid); err == nil {
		t.Fatal("invalid request accepted")
	}
	invalid = input
	invalid.TokenHash = nil
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), invalid); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatal("missing hash accepted")
	}
	invalid = input
	invalid.SourceIP = "bad source"
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), invalid); !errors.Is(err, ErrEnrollmentSource) {
		t.Fatal("invalid source accepted")
	}
	invalid = input
	invalid.Secure = false
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), invalid); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatal("HTTP accepted without opt-in")
	}
	invalid = input
	invalid.TokenHash = agentjoin.DigestToken("unknown")
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), invalid); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatal("unknown credential accepted")
	}
	var id string
	if err := sqlite.DB.QueryRow("SELECT id FROM agent_enrollment_tokens WHERE token_hash=?", token.Hash).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := nodes.RevokeEnrollmentToken(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatal("revoked credential accepted")
	}
}

func TestEnrollmentDatabaseFailuresDoNotCreateNodes(t *testing.T) {
	for _, statement := range []string{
		"DROP TABLE agent_enrollment_tokens",
		"DELETE FROM groups",
		`CREATE TRIGGER reject_enrollment_consume BEFORE UPDATE ON agent_enrollment_tokens BEGIN SELECT RAISE(ABORT,'blocked'); END`,
	} {
		t.Run(statement, func(t *testing.T) {
			sqlite, nodes, _, input := enrollmentFixture(t)
			if _, err := sqlite.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err == nil {
				t.Fatal("expected storage failure")
			}
			var count int
			if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial node: %v", err)
			}
		})
	}
}

func TestEnrollmentIssuanceValidationAndClosedStore(t *testing.T) {
	sqlite, nodes, token, input := enrollmentFixture(t)
	issue := EnrollmentTokenInput{Hash: token.Hash, Prefix: token.Prefix, CreatorID: "owner", ServerURL: "https://beat.example", CreatedAt: input.Now(), ExpiresAt: input.Now().Add(agentjoin.TokenLifetime)}
	if _, err := nodes.CreateEnrollmentToken(t.Context(), issue); err == nil {
		t.Fatal("duplicate hash accepted")
	}
	invalid := issue
	invalid.Hash = nil
	if _, err := nodes.CreateEnrollmentToken(t.Context(), invalid); err == nil {
		t.Fatal("invalid metadata accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := nodes.ConsumeEnrollmentToken(ctx, input); err == nil {
		t.Fatal("canceled join accepted")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.CreateEnrollmentToken(t.Context(), issue); err == nil {
		t.Fatal("closed issuer accepted")
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err == nil {
		t.Fatal("closed consumer accepted")
	}
	if err := nodes.RevokeEnrollmentToken(t.Context(), "missing"); err == nil {
		t.Fatal("closed revoke accepted")
	}
}

func TestEnrollmentMigrationFromVersionSixAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.sqlite")
	sqlite, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("DROP TABLE agent_enrollment_tokens"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("DELETE FROM schema_migrations WHERE version=7"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		sqlite, err = NewSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		var version, count int
		err = sqlite.DB.QueryRow("SELECT MAX(version),COUNT(*) FROM schema_migrations").Scan(&version, &count)
		closeErr := sqlite.Close()
		if err != nil || closeErr != nil || version != currentSchemaVersion || count != currentSchemaVersion {
			t.Fatalf("migration version=%d count=%d errors=%v/%v", version, count, err, closeErr)
		}
	}
}

func TestEnrollmentPrunesExpiredTokens(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	now := input.Now().Add(agentjoin.TokenLifetime)
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	_, err = nodes.CreateEnrollmentToken(t.Context(), EnrollmentTokenInput{Hash: token.Hash, Prefix: token.Prefix, CreatorID: "owner", ServerURL: "http://beat.example", AllowHTTP: true, CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM agent_enrollment_tokens").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired rows retained: %d %v", count, err)
	}
	input.TokenHash = token.Hash
	input.Secure = false
	input.Now = func() time.Time { return now }
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err != nil {
		t.Fatalf("explicit HTTP join failed: %v", err)
	}
}
