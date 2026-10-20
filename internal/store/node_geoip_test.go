package store

import (
	"bytes"
	"context"
	"testing"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
	"github.com/beat/backend/internal/testutil"
)

func TestAgentContactCapturesOnlyAuthorizedSourceAtomically(t *testing.T) {
	database, nodes, contact := agentContactFixture(t)

	secrets := testutil.NewSecrets(t, database.DB)
	nodes.WithGeoIPSecrets(secrets)
	contact.SourceIP = "8.8.8.8"
	if _, err := nodes.AcceptAgentContact(t.Context(), contact); err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	if err := database.DB.QueryRow(`SELECT source_encrypted FROM node_locations WHERE id=?`, contact.Identity.NodeID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	plain, err := secrets.OpenResource(t.Context(), ciphertext, secretbox.GeoIPSourceAAD(contact.Identity.NodeID, "observed"))
	if err != nil || string(plain) != "8.8.8.8" || bytes.Contains(ciphertext, plain) {
		t.Fatal("untrusted source or plaintext", err)
	}
	clear(plain)
	contact.SourceIP = "1.1.1.1"
	contact.Identity.CredentialFingerprint = "revoked"
	if _, err = nodes.AcceptAgentContact(t.Context(), contact); err != ErrAgentCredentialRejected {
		t.Fatal(err)
	}
	var after []byte
	if err = database.DB.QueryRow(`SELECT source_encrypted FROM node_locations WHERE id=?`, contact.Identity.NodeID).Scan(&after); err != nil || !bytes.Equal(ciphertext, after) {
		t.Fatal("rejected identity changed source", err)
	}
	contact.Identity.CredentialFingerprint = model.AgentCredentialFingerprint(bytes.Repeat([]byte("a"), 32))
	if _, err = database.DB.Exec(`CREATE TRIGGER fail_geoip_contact BEFORE UPDATE ON node_locations BEGIN SELECT RAISE(ABORT,'fail');END`); err != nil {
		t.Fatal(err)
	}
	contact.Heartbeat.Host = "must-not-commit"
	if _, err = nodes.AcceptAgentContact(t.Context(), contact); err == nil {
		t.Fatal("failed source committed heartbeat")
	}
	node, err := nodes.GetNode(t.Context(), contact.Identity.NodeID)
	if err != nil || node.Host == "must-not-commit" {
		t.Fatal("non atomic source", err)
	}
}

func TestGeoIPMigrationHonorsCanceledContext(t *testing.T) {
	database := setupTestDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := MigrateGeoIP(ctx, database.DB); err == nil {
		t.Fatal("canceled migration accepted")
	}
}
