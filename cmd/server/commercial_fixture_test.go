package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/backup"
	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/notificationqueue"
	"github.com/beat/backend/internal/store"
	"github.com/beat/backend/internal/testutil"
)

type commercialFixture struct {
	stores    *serverStores
	services  *serverServices
	directory string
}

func newCommercialFixture(t *testing.T) *commercialFixture {
	t.Helper()
	t.Setenv("BEAT_OPERATIONS_ENABLED", "")
	t.Setenv("BEAT_AGENT_ROLLOUT_CONFIG", "")
	t.Setenv(agentRolloutRetentionEnv, "")
	t.Setenv(agentReleaseRetentionEnv, "")
	t.Setenv("BEAT_OPERATIONS_SCANNER_EXECUTABLE", "")
	t.Setenv("BEAT_OPERATIONS_SCANNER_ARGS", "")
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEAT_GEOIP_MMDB_PATH", "")
	stores, err := openServerStores(t.Context(), filepath.Join(directory, "server.db"), filepath.Join(directory, "metrics"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stores.closeIgnoringErrors)
	stores.secrets = testutil.NewSecrets(t, stores.sqlite.DB)
	queue, err := notificationqueue.NewService(notificationqueue.Config{Database: stores.sqlite.DB, Secrets: stores.secrets})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	backups, err := backup.NewService(t.Context(), stores.backups, stores.sqlite, stores.mts, filepath.Join(directory, "backups"), filepath.Join(directory, "admin-data.key"), "0.2.0-dev")
	if err != nil {
		t.Fatal(err)
	}
	services := &serverServices{delivery: queue, backups: backups}
	t.Cleanup(services.closeCommercial)
	return &commercialFixture{stores: stores, services: services, directory: directory}
}

type countedLegacyRules struct{ calls atomic.Int64 }

func (r *countedLegacyRules) ListEnabledRules(context.Context) ([]model.AlertRule, error) {
	r.calls.Add(1)
	return nil, nil
}

func commercialNode(t *testing.T, f *commercialFixture, name string) *model.Node {
	t.Helper()
	node, err := f.stores.nodes.UpsertNode(t.Context(), name, "192.0.2.1", 22)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.stores.sqlite.DB.Exec("UPDATE nodes SET last_seen=? WHERE id=?", time.Now().UTC().Add(-2*time.Minute), node.ID); err != nil {
		t.Fatal(err)
	}
	return node
}

func commercialReceipt(t *testing.T, f *commercialFixture, node *model.Node) {
	t.Helper()
	at := time.Now().UTC().Add(-2 * time.Minute)
	input := store.AgentReportInput{Identity: model.AgentIdentity{NodeID: node.ID, Mode: model.AgentCredentialActive, CredentialFingerprint: strings.Repeat("a", 64)}, Now: at,
		Report: model.AgentReport{ProtocolVersion: model.AgentReportProtocol, SampleID: uuid.NewString(), CapturedAt: at, ReportIntervalMS: (5 * time.Minute).Milliseconds(), Host: "192.0.2.1", Port: 22, Metrics: &model.NodeMetrics{}}}
	attempt, err := f.stores.mts.PrepareAgentReport(t.Context(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()
	if _, err := attempt.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func awaitCommercial(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("commercial worker did not make progress")
		}
	}
}
