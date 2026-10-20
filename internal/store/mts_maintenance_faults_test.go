package store

import (
	"context"
	"testing"
	"time"
)

// TestMTSOperationCancellationFaults 覆盖指标操作锁与标量查询在取消上下文下的失败关闭。
func TestMTSOperationCancellationFaults(t *testing.T) {
	metrics := metricQueryMTS(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := metrics.QueryLatestFiltered(ctx, "node", nil); err == nil {
		t.Fatal("canceled query took the operation lock")
	}
	if err := metrics.CleanupBefore(ctx, time.Now().Add(-time.Hour)); err == nil {
		t.Fatal("canceled cleanup took the operation lock")
	}
	if _, err := metrics.queryLatestFilteredLocked(ctx, "node", nil); err == nil {
		t.Fatal("canceled scalar query succeeded")
	}
}

// TestMTSQueryLatestFilteredSkipsDisallowedMetrics 覆盖允许清单裁剪掉的指标被跳过。
func TestMTSQueryLatestFilteredSkipsDisallowedMetrics(t *testing.T) {
	metrics := metricQueryMTS(t)
	allowed := map[string]bool{}
	for _, metric := range knownMetrics {
		allowed[metric] = metric != knownMetrics[0]
	}
	values, err := metrics.queryLatestFilteredLocked(t.Context(), "node", allowed)
	if err != nil {
		t.Fatalf("filtered latest query error = %v", err)
	}
	if len(values) != 0 {
		t.Fatalf("filtered latest query = %v", values)
	}
}

// TestMTSReleaseOwnerWithoutLease 覆盖没有租约时的释放空操作。
func TestMTSReleaseOwnerWithoutLease(t *testing.T) {
	var store MTSStore
	if err := store.releaseOwner(); err != nil {
		t.Fatalf("lease-less release error = %v", err)
	}
}

// TestMTSDiskUsageUnreadablePath 覆盖磁盘占用统计在路径不可读时的失败关闭。
func TestMTSDiskUsageUnreadablePath(t *testing.T) {
	store := &MTSStore{engine: &trackedMTSEngine{}, path: "/nonexistent-mts-disk-usage-path"}
	if _, err := store.DiskUsage(); err == nil {
		t.Fatal("missing MTS path reported a disk usage")
	}
}
