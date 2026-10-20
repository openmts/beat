package store

import (
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

// C05/C14 的已知限制：保留清理此前只判定「查询再也看不到旧行」，没有校验磁盘是否真的回收。
// 引擎的删除是 tombstone，磁盘回收依赖后续 compaction，所以这里断言占用真的下降、
// 旧窗口为空、保留窗口内的样本仍在——否则长期运行的实例只会越涨越大。
func TestMetricRetentionReclaimsExpiredShards(t *testing.T) {
	metrics := setupTestMTS(t)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	expired := now.AddDate(0, 0, -30)
	seedRetentionSamples(t, metrics, expired, 900)
	seedRetentionSamples(t, metrics, now.Add(-30*time.Minute), 900)
	flushRetentionSamples(t, metrics)

	before := retentionDiskUsage(t, metrics)
	beforePacks := retentionPackFiles(t, metrics)
	if beforePacks != 2 {
		t.Fatalf("seeded shards: want 2 pack files, got %d", beforePacks)
	}
	beforeWAL := retentionWALBytes(t, metrics)

	applyRetention(t, metrics, model.MetricRetentionJob{Key: "cpu", RetentionDays: 1, StartedAt: now})

	after := retentionDiskUsage(t, metrics)
	if after >= before {
		t.Fatalf("retention did not reclaim disk: before=%d after=%d", before, after)
	}
	if packs := retentionPackFiles(t, metrics); packs != 1 {
		t.Fatalf("expired shard still holds a packed segment: want 1 pack file, got %d", packs)
	}
	// 清掉的 900 行不能换一种形式留在未压缩 WAL 里。
	if afterWAL := retentionWALBytes(t, metrics); afterWAL > beforeWAL {
		t.Fatalf("retention grew the write-ahead log: before=%d after=%d", beforeWAL, afterWAL)
	}
	assertRetentionRows(t, metrics, expired.Add(-time.Hour), expired.Add(time.Hour), 0)
	assertRetentionRows(t, metrics, now.Add(-time.Hour), now, 900)
}

// 半过期分片走的是另一条路径：删除只覆盖分片的一部分，compaction 必须重写这个分片
// 并丢掉 tombstone，而不是留下「查询看不到、磁盘还在」的旧字节。
func TestMetricRetentionCompactsPartiallyExpiredShard(t *testing.T) {
	metrics := setupTestMTS(t)
	cutoff := time.Date(2026, 10, 18, 12, 30, 0, 0, time.UTC)
	seedRetentionSamples(t, metrics, cutoff.Add(-25*time.Minute), 1440)
	seedRetentionSamples(t, metrics, cutoff.Add(time.Minute), 1500)
	flushRetentionSamples(t, metrics)

	before := retentionDiskUsage(t, metrics)
	if packs := retentionPackFiles(t, metrics); packs != 1 {
		t.Fatalf("seeded samples should share one shard: got %d pack files", packs)
	}

	applyRetention(t, metrics, model.MetricRetentionJob{Key: "cpu", RetentionDays: 1, StartedAt: cutoff.AddDate(0, 0, 1)})

	after := retentionDiskUsage(t, metrics)
	if after*2 >= before {
		t.Fatalf("tombstoned rows were not rewritten away: before=%d after=%d", before, after)
	}
	assertRetentionRows(t, metrics, cutoff.Add(-25*time.Minute), cutoff, 0)
	assertRetentionRows(t, metrics, cutoff.Add(time.Minute), cutoff.Add(26*time.Minute), 1500)
}

func seedRetentionSamples(t *testing.T, metrics *MTSStore, start time.Time, count int) {
	t.Helper()
	for index := range count {
		at := start.Add(time.Duration(index) * time.Second)
		if err := metrics.WriteMetric(t.Context(), "retention-node", "cpu", float64(index), at); err != nil {
			t.Fatalf("write sample %d at %s: %v", index, at, err)
		}
	}
}

func flushRetentionSamples(t *testing.T, metrics *MTSStore) {
	t.Helper()
	if err := metrics.Flush(t.Context()); err != nil {
		t.Fatalf("flush seeded samples: %v", err)
	}
}

func applyRetention(t *testing.T, metrics *MTSStore, job model.MetricRetentionJob) {
	t.Helper()
	if err := metrics.ApplyMetricRetention(t.Context(), job); err != nil {
		t.Fatalf("apply retention: %v", err)
	}
}

func retentionDiskUsage(t *testing.T, metrics *MTSStore) int64 {
	t.Helper()
	usage, err := metrics.DiskUsage()
	if err != nil {
		t.Fatalf("measure disk usage: %v", err)
	}
	if usage <= 0 {
		t.Fatalf("seeded store reports %d bytes", usage)
	}
	return usage
}

// retentionPackFiles 数的是已落盘的 packed segment：整分片过期时它必须消失。
func retentionPackFiles(t *testing.T, metrics *MTSStore) int {
	t.Helper()
	found := 0
	err := filepath.WalkDir(metrics.path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() && entry.Name() == "pack.bin" {
			found++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk store: %v", err)
	}
	return found
}

// retentionWALBytes 统计未压缩预写日志的总字节数：compaction 之后它应当只保留表头。
func retentionWALBytes(t *testing.T, metrics *MTSStore) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(metrics.path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".wal" {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk store: %v", err)
	}
	return total
}

func assertRetentionRows(t *testing.T, metrics *MTSStore, from, to time.Time, want int) {
	t.Helper()
	rows, err := metrics.QueryMetrics(t.Context(), []string{"cpu"}, from, to, "retention-node")
	if err != nil {
		t.Fatalf("query %s..%s: %v", from, to, err)
	}
	if got := len(rows["cpu"]); got != want {
		t.Fatalf("rows in %s..%s: want %d, got %d", from, to, want, got)
	}
}
