package store

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
)

func enrollmentFixture(t *testing.T) (*SQLiteStore, *NodeStore, agentjoin.Token, EnrollmentConsumeInput) {
	t.Helper()
	sqlite := setupTestDB(t)
	nodes := NewNodeStore(sqlite.DB)
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	_, err = nodes.CreateEnrollmentToken(t.Context(), EnrollmentTokenInput{
		Hash: token.Hash, Prefix: token.Prefix, ServerURL: "https://beat.example", CreatorID: "owner",
		CreatedAt: now, ExpiresAt: now.Add(agentjoin.TokenLifetime),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := EnrollmentConsumeInput{TokenHash: token.Hash, SourceIP: "192.0.2.10", Secure: true,
		Now: func() time.Time { return now }, AuditRequestID: "http-request",
		Request: agentjoin.Request{RequestID: "request-1", AgentToken: credential.Plaintext, NodeName: "joined-node", SSHPort: 22}}
	return sqlite, nodes, token, input
}

func TestEnrollmentConsumptionIsIdempotentAndPrivate(t *testing.T) {
	sqlite, nodes, token, input := enrollmentFixture(t)
	input.AuditRequestID = ""
	first, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil || second != first {
		t.Fatalf("retry differs: %v", err)
	}
	if first.NodeID == "" || first.Config.AdvertisedHost != input.SourceIP || first.Config.ReportInterval != "5s" {
		t.Fatal("incomplete configuration")
	}
	node, err := nodes.AuthenticateAgentToken(t.Context(), input.Request.AgentToken)
	if err != nil || node == nil || node.ID != first.NodeID || node.GroupID == "" {
		t.Fatalf("node authentication: %v", err)
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("node count %d: %v", count, err)
	}
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM admin_audit_events WHERE action='agent.enrollment.consume'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count %d: %v", count, err)
	}
	for _, secret := range []string{token.Plaintext, input.Request.AgentToken} {
		if err := sqlite.DB.QueryRow(`SELECT COUNT(*) FROM agent_enrollment_tokens WHERE
			CAST(token_hash AS TEXT)=? OR token_prefix=? OR CAST(agent_token_hash AS TEXT)=?`, secret, secret, secret).Scan(&count); err != nil || count != 0 {
			t.Fatalf("credential persisted: %v", err)
		}
	}
	input.Request.NodeName = "ignored-on-retry"
	input.Request.AdvertisedHost = "192.0.2.99"
	third, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil || third != first {
		t.Fatalf("retry altered original configuration: %v", err)
	}
}

func TestEnrollmentRejectsOtherClientsAndExpiredCredentials(t *testing.T) {
	_, nodes, _, input := enrollmentFixture(t)
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	other := input
	other.Request.RequestID = "another-client"
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), other); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatalf("other request: %v", err)
	}
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	other = input
	other.Request.AgentToken = credential.Plaintext
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), other); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatalf("other credential: %v", err)
	}
	expires := input.Now().Add(agentjoin.TokenLifetime)
	input.Now = func() time.Time { return expires }
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); !errors.Is(err, ErrEnrollmentUnauthorized) {
		t.Fatalf("expired retry: %v", err)
	}
}

func TestEnrollmentCannotUndoNodeCredentialLifecycle(t *testing.T) {
	for _, operation := range []string{"rotate", "revoke", "delete"} {
		t.Run(operation, func(t *testing.T) {
			_, nodes, _, input := enrollmentFixture(t)
			result, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "rotate":
				credential, generateErr := agentcredential.Generate()
				if generateErr != nil {
					t.Fatal(generateErr)
				}
				_, err = nodes.RotateAgentToken(t.Context(), result.NodeID, AgentCredential{Hash: credential.Hash, Prefix: credential.Prefix, CreatedAt: input.Now()})
			case "revoke":
				_, err = nodes.RevokeAgentToken(t.Context(), result.NodeID, input.Now())
			case "delete":
				err = nodes.DeleteNode(t.Context(), result.NodeID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); !errors.Is(err, ErrEnrollmentIdentityUnavailable) {
				t.Fatalf("credential lifecycle bypass: %v", err)
			}
		})
	}
}

func TestEnrollmentNameConflictDoesNotConsumeToken(t *testing.T) {
	_, nodes, _, input := enrollmentFixture(t)
	if _, err := nodes.UpsertNode(t.Context(), input.Request.NodeName, "192.0.2.1", 22); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); !errors.Is(err, ErrNodeNameConflict) {
		t.Fatalf("name conflict: %v", err)
	}
	input.Request.NodeName = "different-node"
	input.Request.AdvertisedHost = "node.example"
	result, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil || result.Config.AdvertisedHost != "node.example" {
		t.Fatalf("retry renamed node: %v", err)
	}
}

func TestEnrollmentConcurrentClientsConsumeOnce(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	const clients = 8
	var wg sync.WaitGroup
	results := make(chan error, clients)
	for index := range clients {
		candidate := input
		candidate.Request.RequestID += string(rune('a' + index))
		wg.Go(func() { _, err := nodes.ConsumeEnrollmentToken(t.Context(), candidate); results <- err })
	}
	wg.Wait()
	close(results)
	var success int
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrEnrollmentUnauthorized) {
			t.Fatal(err)
		}
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != 1 || success != 1 {
		t.Fatalf("success=%d nodes=%d error=%v", success, count, err)
	}
}

func TestEnrollmentAuditFailureRollsBackNodeAndConsumption(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	if _, err := sqlite.DB.Exec(`CREATE TRIGGER fail_enrollment_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err == nil {
		t.Fatal("expected audit failure")
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != 0 {
		t.Fatalf("node was committed: %v", err)
	}
	if _, err := sqlite.DB.Exec("DROP TRIGGER fail_enrollment_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); err != nil {
		t.Fatal(err)
	}
}
