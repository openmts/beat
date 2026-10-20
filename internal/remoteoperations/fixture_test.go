package remoteoperations

import (
	"bytes"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type fixture struct {
	secretsDirectory                string
	service                         *Service
	database                        *store.SQLiteStore
	actor                           Actor
	nodeID, token, digest, policyID string
	policy                          Policy
}

func testFixture(t *testing.T) fixture {
	t.Helper()
	db, err := store.NewSQLiteStore(operationsFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	secretsDirectory := t.TempDir()
	secrets := testutil.OpenSecrets(t, db.DB, secretsDirectory)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: db.DB, Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	service, err := NewService(Config{Database: db.DB, Secrets: secrets, Queue: queue, Enabled: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	f := fixture{secretsDirectory: secretsDirectory, service: service, database: db, actor: testActor(t, db, now), policy: testPolicy()}
	f.nodeID, f.token = testNode(t, db, now)
	f.digest, err = service.Advertise(t.Context(), NodeProof{Token: f.token}, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	f.policyID, err = service.SavePolicy(t.Context(), f.actor, PolicyInput{Name: "Safe policy", Policy: f.policy, NodeIDs: []string{f.nodeID}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func testPolicy() Policy {
	return Policy{SupportedShells: []string{"sh"}, Enabled: true, Kinds: []string{"shell"}, MaximumTargets: 50, MaximumActiveTasks: 5, Concurrency: 1, TimeoutSeconds: 300, OutputBytes: 256 * 1024}
}

func testActor(t *testing.T, db *store.SQLiteStore, now time.Time) Actor {
	t.Helper()
	until := now.Add(time.Hour)
	admins := store.NewAdminStore(db.DB)
	if err := admins.CreateUser(t.Context(), &model.AdminUser{ID: "owner", Username: "owner", DisplayName: "Owner", Role: model.AdminRoleOwner, Enabled: true,
		PasswordHash: "test", PasswordChangedAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: "session", UserID: "owner", TokenHash: bytes.Repeat([]byte{4}, 32), TokenPrefix: "test", CreatedAt: now,
		LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
		t.Fatal(err)
	}
	return Actor{SessionID: "session", UserID: "owner"}
}

func testNode(t *testing.T, db *store.SQLiteStore, now time.Time) (string, string) {
	t.Helper()
	credential, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.NewNodeStore(db.DB).CreateManagedNode(t.Context(), store.ManagedNodeInput{Name: "fixture", Host: "127.0.0.1", Port: 22}, store.AgentCredential{Hash: credential.Hash[:], Prefix: credential.Prefix, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return node.ID, credential.Plaintext
}

func (f fixture) task(t *testing.T, key string) string {
	t.Helper()
	id, err := f.service.CreateTask(t.Context(), f.actor, f.input(key))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f fixture) input(key string) TaskInput {
	return TaskInput{DedupeKey: key, PolicyID: f.policyID, Command: []byte("printf private-output"), NodeIDs: []string{f.nodeID}, Limits: Limits{Kind: "shell", Targets: 1, TimeoutSeconds: 30, OutputBytes: 1024}}
}

func (f fixture) lease(t *testing.T) AttemptRequest {
	t.Helper()
	lease, err := f.service.Lease(t.Context(), LeaseRequest{Proof: NodeProof{Token: f.token}, CapabilityDigest: f.digest})
	if err != nil || lease == nil {
		t.Fatalf("lease: %v", err)
	}
	return AttemptRequest{Node: NodeProof{Token: f.token}, ID: lease.AttemptID, Nonce: lease.Nonce}
}

func (f fixture) sql(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.database.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
