package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentjoin"
)

type enrollmentQueryFault struct {
	schemaExecutor
	closed *sql.DB
}

func (fault enrollmentQueryFault) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return fault.closed.QueryRowContext(ctx, query, args...)
}

func TestEnrollmentCapacityAndQueryFailures(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	_, err := sqlite.DB.Exec(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<?)
		INSERT INTO agent_enrollment_tokens(id,token_hash,token_prefix,server_url,allow_http,creator_id,created_at,expires_at)
		SELECT 'capacity-'||n,CAST(printf('%032d',n) AS BLOB),'test','https://beat.example',0,'owner',?,? FROM seq`,
		maximumEnrollmentTokens-1, input.Now(), input.Now().Add(agentjoin.TokenLifetime))
	if err != nil {
		t.Fatal(err)
	}
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	_, err = nodes.CreateEnrollmentToken(t.Context(), EnrollmentTokenInput{Hash: token.Hash, Prefix: token.Prefix,
		ServerURL: "https://beat.example", CreatorID: "owner", CreatedAt: input.Now(), ExpiresAt: input.Now().Add(agentjoin.TokenLifetime)})
	if !errors.Is(err, ErrEnrollmentCapacity) {
		t.Fatalf("capacity error: %v", err)
	}
	closed, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reserveEnrollmentCapacity(t.Context(), enrollmentQueryFault{schemaExecutor: sqlite.DB, closed: closed}, input.Now()); err == nil {
		t.Fatal("query fault ignored")
	}
	if err := applyAgentEnrollment(t.Context(), closed); err == nil {
		t.Fatal("migration fault ignored")
	}
}

func TestEnrollmentPruneFailureDoesNotIssueCredential(t *testing.T) {
	sqlite, nodes, token, input := enrollmentFixture(t)
	if _, err := sqlite.DB.Exec(`CREATE TRIGGER reject_enrollment_prune BEFORE DELETE ON agent_enrollment_tokens BEGIN SELECT RAISE(ABORT,'blocked'); END`); err != nil {
		t.Fatal(err)
	}
	now := input.Now().Add(agentjoin.TokenLifetime)
	_, err := nodes.CreateEnrollmentToken(t.Context(), EnrollmentTokenInput{Hash: token.Hash, Prefix: token.Prefix, ServerURL: "https://beat.example", CreatorID: "owner", CreatedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err == nil {
		t.Fatal("prune error ignored")
	}
}

func TestEnrollmentIdentityStorageFailuresRollback(t *testing.T) {
	for _, statement := range []string{
		"DROP TABLE agent_enrollment_identities",
		"DROP TABLE nodes",
		`CREATE TRIGGER reject_enrollment_binding BEFORE INSERT ON agent_enrollment_identities BEGIN SELECT RAISE(ABORT,'blocked'); END`,
	} {
		t.Run(statement, func(t *testing.T) {
			sqlite, nodes, _, input := enrollmentFixture(t)
			if _, err := sqlite.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err == nil {
				t.Fatal("identity failure ignored")
			}
			var consumed int
			if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM agent_enrollment_tokens WHERE consumed_at IS NOT NULL").Scan(&consumed); err != nil || consumed != 0 {
				t.Fatal("failed enrollment was consumed")
			}
		})
	}
}

func TestEnrollmentCommitFailuresAreAtomic(t *testing.T) {
	for _, target := range []string{"agent_enrollment_tokens", "admin_audit_events"} {
		t.Run(target, func(t *testing.T) {
			sqlite, nodes, token, input := enrollmentFixture(t)
			for _, statement := range []string{
				"CREATE TABLE enrollment_fault_parent(id TEXT PRIMARY KEY)",
				"CREATE TABLE enrollment_fault_child(parent TEXT REFERENCES enrollment_fault_parent(id) DEFERRABLE INITIALLY DEFERRED)",
				"CREATE TRIGGER enrollment_commit_failure AFTER INSERT ON " + target + " BEGIN INSERT INTO enrollment_fault_child VALUES('missing'); END",
			} {
				if _, err := sqlite.DB.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			if target == "admin_audit_events" {
				if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err == nil {
					t.Fatal("join commit failure ignored")
				}
			} else {
				_, err := nodes.CreateEnrollmentToken(t.Context(), EnrollmentTokenInput{Hash: agentjoin.DigestToken(token.Plaintext + "new"), Prefix: token.Prefix, ServerURL: "https://beat.example", CreatorID: "owner", CreatedAt: input.Now(), ExpiresAt: input.Now().Add(time.Minute)})
				if err == nil {
					t.Fatal("issuance commit failure ignored")
				}
			}
			var count int
			if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != 0 {
				t.Fatal("commit failure retained a node")
			}
		})
	}
}

func TestEnrollmentResumePropagatesStorageFailure(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("ALTER TABLE nodes RENAME COLUMN agent_token_created_at TO broken_column"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err == nil {
		t.Fatal("resume query error ignored")
	}
}
