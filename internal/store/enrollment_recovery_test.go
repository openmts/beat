package store

import (
	"errors"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/agentjoin"
)

func freshEnrollmentInput(t *testing.T, nodes *NodeStore, input EnrollmentConsumeInput) EnrollmentConsumeInput {
	t.Helper()
	token, err := agentjoin.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	now := input.Now().Add(agentjoin.TokenLifetime + time.Minute)
	_, err = nodes.CreateEnrollmentToken(t.Context(), EnrollmentTokenInput{Hash: token.Hash, Prefix: token.Prefix,
		ServerURL: "https://beat.example", CreatorID: "owner", CreatedAt: now, ExpiresAt: now.Add(agentjoin.TokenLifetime)})
	if err != nil {
		t.Fatal(err)
	}
	input.TokenHash = token.Hash
	input.Now = func() time.Time { return now }
	return input
}

func TestFreshGrantRecoversUnconsumedExpiredRequest(t *testing.T) {
	_, nodes, _, input := enrollmentFixture(t)
	input = freshEnrollmentInput(t, nodes, input)
	result, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil || result.NodeID == "" {
		t.Fatalf("fresh grant could not recover request: %v", err)
	}
}

func TestFreshGrantRecoversLostResponseWithoutChangingNode(t *testing.T) {
	sqlite, nodes, _, input := enrollmentFixture(t)
	first, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("UPDATE nodes SET host='node.updated.example',port=2222 WHERE id=?", first.NodeID); err != nil {
		t.Fatal(err)
	}
	input = freshEnrollmentInput(t, nodes, input)
	input.Request.NodeName = "must-not-rename"
	input.Request.AdvertisedHost = "must-not-update.example"
	result, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil || result.NodeID != first.NodeID || result.Config.NodeName != first.Config.NodeName ||
		result.Config.AdvertisedHost != "node.updated.example" || result.Config.SSHPort != 2222 {
		t.Fatalf("recovery changed identity: %v", err)
	}
	retry, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
	if err != nil || retry != result {
		t.Fatalf("fresh grant retry rejected original credential timestamp: %v", err)
	}
	var count int
	if err := sqlite.DB.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count); err != nil || count != 1 {
		t.Fatalf("recovery duplicated node: %v", err)
	}
}

func TestFreshGrantCannotReviveRevokedRotatedOrDeletedIdentity(t *testing.T) {
	for _, operation := range []string{"rotate", "revoke", "delete"} {
		t.Run(operation, func(t *testing.T) {
			_, nodes, _, input := enrollmentFixture(t)
			first, err := nodes.ConsumeEnrollmentToken(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "rotate":
				replacement, generateErr := agentcredential.Generate()
				if generateErr != nil {
					t.Fatal(generateErr)
				}
				_, err = nodes.RotateAgentToken(t.Context(), first.NodeID, AgentCredential{Hash: replacement.Hash, Prefix: replacement.Prefix, CreatedAt: input.Now().Add(time.Second)})
			case "revoke":
				_, err = nodes.RevokeAgentToken(t.Context(), first.NodeID, input.Now())
			case "delete":
				err = nodes.DeleteNode(t.Context(), first.NodeID)
			}
			if err != nil {
				t.Fatal(err)
			}
			input = freshEnrollmentInput(t, nodes, input)
			if _, err := nodes.ConsumeEnrollmentToken(t.Context(), input); !errors.Is(err, ErrEnrollmentIdentityUnavailable) {
				t.Fatalf("revived %s identity: %v", operation, err)
			}
		})
	}
}
