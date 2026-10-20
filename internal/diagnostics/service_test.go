//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beat/backend/internal/agentcredential"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type fixture struct {
	service  *Service
	database *store.SQLiteStore
	metrics  *recordingMetrics
	owner    Actor
	nodeID   string
	token    string
	now      time.Time
}

type recordingMetrics struct {
	mu            sync.Mutex
	batches       []store.DiagnosticSampleBatch
	queries       []store.DiagnosticQuery
	points        []store.TimePoint
	deleted       []string
	writeFailure  error
	deleteFailure error
}

func (metrics *recordingMetrics) WriteDiagnosticSamples(_ context.Context, batch store.DiagnosticSampleBatch) error {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.batches = append(metrics.batches, batch)
	return metrics.writeFailure
}

func (metrics *recordingMetrics) ReadDiagnosticPoints(_ context.Context, query store.DiagnosticQuery) ([]store.TimePoint, error) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.queries = append(metrics.queries, query)
	return metrics.points, nil
}

func (metrics *recordingMetrics) DeleteDiagnosticJob(_ context.Context, jobID string) error {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.deleted = append(metrics.deleted, jobID)
	return metrics.deleteFailure
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	metrics := &recordingMetrics{}
	service, err := NewService(Config{Database: database.DB, Secrets: testutil.NewSecrets(t, database.DB),
		Metrics: metrics, Labels: fixtureLabels{}, Resolve: fixtureResolver, Enabled: true,
		Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := agentcredential.Generate()
	if err != nil {
		t.Fatal(err)
	}
	insertFixtureActors(t, database, token)
	return &fixture{service: service, database: database, metrics: metrics, nodeID: "node-a", token: token.Plaintext,
		now: now, owner: Actor{UserID: "owner", Role: "owner", SessionID: "session-owner"}}
}

type fixtureLabels struct{}

func (fixtureLabels) NodeName(_ context.Context, id string) (string, error) {
	if id != "node-a" {
		return "", errors.New("unknown node")
	}
	return "Node A", nil
}

func fixtureResolver(_ context.Context, host string) ([]netip.Addr, error) {
	if host != "diagnostics.example.test" {
		return nil, errors.New("unknown host")
	}
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func TestServiceRequiresEnabledDeployment(t *testing.T) {
	directory := t.TempDir()
	database, err := store.NewSQLiteStore(filepath.Join(directory, "beat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	service, err := NewService(Config{Database: database.DB, Secrets: testutil.NewSecrets(t, database.DB),
		Metrics: &recordingMetrics{}})
	if err != nil {
		t.Fatal(err)
	}
	if service.Enabled() {
		t.Fatal("diagnostics must default to disabled")
	}
	if _, err := service.SavePolicy(context.Background(), Actor{}, model.DiagnosticPolicy{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled service error = %v", err)
	}
	if _, err := service.Lease(context.Background(), LeaseRequest{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled lease error = %v", err)
	}
}

func TestServiceRejectsIncompleteConfiguration(t *testing.T) {
	if _, err := NewService(Config{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("missing dependencies must be rejected")
	}
}

func insertFixtureActors(t *testing.T, database *store.SQLiteStore, token agentcredential.Token) {
	t.Helper()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if _, err := database.DB.Exec(`INSERT INTO admin_users(id, username, display_name, role, password_hash, enabled,
		password_changed_at, created_at, updated_at) VALUES('owner','owner','Owner','owner','x',1,?,?,?)`,
		now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO admin_users(id, username, display_name, role, password_hash, enabled,
		password_changed_at, created_at, updated_at) VALUES('admin','admin','Admin','admin','x',1,?,?,?)`,
		now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO admin_sessions(id, user_id, token_hash, token_prefix, created_at,
		last_activity_at, idle_expires_at, absolute_expires_at, reauthenticated_until, ip_address, user_agent)
		VALUES('session-owner','owner','hash-owner','prefix',?,?,?,?,?, '127.0.0.1','fixture')`,
		now, now, now.Add(time.Hour), now.Add(time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`INSERT INTO nodes(id, name, host, port, created_at, updated_at, agent_token_hash,
		agent_token_created_at) VALUES('node-a','Node A','93.184.216.34',22,?,?,?,?)`,
		now, now, token.Hash[:], now); err != nil {
		t.Fatal(err)
	}
}
