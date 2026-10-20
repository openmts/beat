package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/openmts/mts"
)

func TestCompactionDegradedMatchesEngineReasons(t *testing.T) {
	tests := []struct {
		name    string
		reasons []string
		want    bool
	}{
		{name: "no reasons", reasons: nil, want: false},
		{name: "unrelated reason", reasons: []string{"wal unavailable on shard beat/autogen/1"}, want: false},
		{name: "truncated prefix", reasons: []string{"compaction degraded on shard"}, want: false},
		{name: "degraded shard", reasons: []string{"compaction degraded on shard beat/autogen/2"}, want: true},
		{
			name:    "degraded after other reasons",
			reasons: []string{"disk check failed on shard beat/autogen/3", "compaction degraded on shard beat/autogen/3"},
			want:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := compactionDegraded(test.reasons); got != test.want {
				t.Fatalf("compactionDegraded(%v) = %v, want %v", test.reasons, got, test.want)
			}
		})
	}
}

func TestMTSRepairCompactionIfDegradedHealsOverlappingParts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics")
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	seedUncompactedHistory(t, path, base)
	store := openDegradedMTSStore(t, path)

	healthy, reasons := store.Health()
	if healthy || !compactionDegraded(reasons) {
		t.Fatalf("fixture health = %v %v, want degraded compaction", healthy, reasons)
	}
	repaired, err := store.RepairCompactionIfDegraded(t.Context())
	if err != nil || !repaired {
		t.Fatalf("repair = %v, err = %v, want repaired", repaired, err)
	}
	assertCompactedHistory(t, store, base)
	repaired, err = store.RepairCompactionIfDegraded(t.Context())
	if err != nil || repaired {
		t.Fatalf("second repair = %v, err = %v, want no-op", repaired, err)
	}
}

func TestMTSRepairCompactionIfDegradedLeavesHealthyStoreUntouched(t *testing.T) {
	store := setupTestMTS(t)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	nodeID, metric := "repair-node", "cpu"
	if err := store.WriteMetric(t.Context(), nodeID, metric, 7, base); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	repaired, err := store.RepairCompactionIfDegraded(t.Context())
	if err != nil || repaired {
		t.Fatalf("healthy repair = %v, err = %v, want skipped", repaired, err)
	}
	rows, err := store.QueryMetrics(t.Context(), []string{metric}, base.Add(-time.Second), base.Add(time.Second), nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []TimePoint{{Timestamp: base, Value: 7}}; !reflect.DeepEqual(want, rows[metric]) {
		t.Fatalf("history after skipped repair: want %+v, got %+v", want, rows[metric])
	}
}

func TestMTSRepairCompactionIfDegradedHonorsCancelledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics")
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	seedUncompactedHistory(t, path, base)
	store := openDegradedMTSStore(t, path)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repaired, err := store.RepairCompactionIfDegraded(ctx)
	if repaired || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled repair = %v, err = %v, want context.Canceled", repaired, err)
	}
}

func TestMTSRepairCompactionLockedRequiresOpenStore(t *testing.T) {
	store := setupTestMTS(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	repaired, err := store.repairCompactionLocked(context.Background())
	if repaired || !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatalf("closed repair = %v, err = %v, want %v", repaired, err, ErrMTSStoreClosed)
	}
	repaired, err = store.RepairCompactionIfDegraded(t.Context())
	if repaired || err != nil {
		t.Fatalf("closed repair-if-degraded = %v, err = %v, want skipped", repaired, err)
	}
}

// openDegradedMTSStore 用保持整理关闭的引擎打开既有重叠 part 的数据目录并手工装配 store，
// 避开「NewMTSStore 打开即 flush 顺带自愈」，从而验证按需整理的检测与修复路径。
func openDegradedMTSStore(t *testing.T, path string) *MTSStore {
	t.Helper()
	owner, err := acquireMTSOwnerLease(path)
	if err != nil {
		t.Fatalf("acquire MTS owner lease: %v", err)
	}
	options := mts.DefaultOptions(path)
	options.DefaultDatabase = "beat"
	engine, err := mts.Open(t.Context(), options)
	if err != nil {
		_ = owner.Close()
		t.Fatalf("open degraded MTS engine: %v", err)
	}
	store := &MTSStore{engine: &trackedMTSEngine{Engine: engine}, owner: owner, path: path}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return store
}
