package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

func TestNodeServiceStoreErrors(t *testing.T) {
	s, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	mts, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create mts store: %v", err)
	}
	t.Cleanup(func() { _ = mts.Close() })

	svc := NewNodeService(store.NewNodeStore(s.DB), mts)
	if _, err := svc.RegisterNode(context.Background(), "host", 22, nil); err == nil {
		t.Fatal("expected node store error")
	}
}

func TestNodeServiceRejectsInvalidMetric(t *testing.T) {
	svc := setupTestNodeService(t)
	now := time.Now()
	if _, err := svc.GetNodeMetrics(context.Background(), "node", []string{""}, now.Add(-time.Hour), now); err == nil {
		t.Fatal("expected invalid metric error")
	}
}

func TestNodeServiceMetricWriteError(t *testing.T) {
	s, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	mts, err := store.NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("create mts store: %v", err)
	}
	if err := mts.Close(); err != nil {
		t.Fatalf("close mts store: %v", err)
	}

	svc := NewNodeService(store.NewNodeStore(s.DB), mts)
	if _, err := svc.RegisterNode(context.Background(), "host", 22, &model.NodeMetrics{CPU: 1}); err == nil {
		t.Fatal("expected metric write error")
	}
}

func TestSSHKeyServiceStoreErrors(t *testing.T) {
	s, err := store.NewSQLiteStore("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("create sqlite store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	svc := NewSSHKeyService(store.NewSSHKeyStore(s.DB))
	ctx := context.Background()

	if _, err := svc.CreateKey(ctx, "key", "ed25519"); err == nil {
		t.Fatal("expected create error")
	}
	if _, err := svc.ListKeys(ctx); err == nil {
		t.Fatal("expected list error")
	}
	if err := svc.DeleteKey(ctx, "id"); err == nil {
		t.Fatal("expected delete error")
	}
}
