package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// TestMetricErasureExcludesMetricSnapshot 证明备份与擦除共用同一把独占锁，
// 二者不可能观察到彼此的半成品快照。
func TestMetricErasureExcludesMetricSnapshot(t *testing.T) {
	mts, err := NewMTSStore(filepath.Join(t.TempDir(), "mts"))
	if err != nil {
		t.Fatalf("open MTS store: %v", err)
	}
	t.Cleanup(func() { _ = mts.Close() })

	session, err := mts.BeginMetricErasure(t.Context(), model.MetricErasureFleetTelemetry, "", "")
	if err != nil || session == nil {
		t.Fatalf("begin metric erasure = %v, err = %v", session, err)
	}

	blocked, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := mts.BeginMetricSnapshot(blocked); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("snapshot during erasure error = %v", err)
	}
	session.Close()

	snapshot, err := mts.BeginMetricSnapshot(t.Context())
	if err != nil || snapshot == nil {
		t.Fatalf("snapshot after erasure = %v, err = %v", snapshot, err)
	}
	snapshot.Close()
}
