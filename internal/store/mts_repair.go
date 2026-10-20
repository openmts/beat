package store

import (
	"context"
	"fmt"
	"strings"
)

// compactionDegradedReason 是上游引擎在分片存在待整理重叠 part 时给出的健康原因前缀，
// 形状为 "compaction degraded on shard <id>"。
const compactionDegradedReason = "compaction degraded on shard "

// RepairCompactionIfDegraded 在健康原因报出分片压缩退化时执行一次受独占操作锁保护的
// flush + 整库整理，并返回是否真的执行了整理。它不删除保留窗口内的数据，也不改动就绪口径。
//
// 背景：活跃分片的 flush 期整理一旦被在读引用挡住就会被跳过，且引擎不重试，于是重叠 part
// 会持续把就绪探针压成 503，直到下一次每日维护或重启（见 v220 记录）。这里提供与每日维护
// 同级别的受守卫整理，供维护调度器按需触发。
func (s *MTSStore) RepairCompactionIfDegraded(ctx context.Context) (bool, error) {
	healthy, reasons := s.Health()
	if healthy || !compactionDegraded(reasons) {
		return false, nil
	}
	unlock, err := s.lockMetricOperation(ctx, true)
	if err != nil {
		return false, err
	}
	defer unlock()
	return s.repairCompactionLocked(ctx)
}

// repairCompactionLocked 在已持有独占操作锁时执行整理；整理对只剩一个 part 且没有墓碑的
// 分片是快速空操作，因此与写入路径的 flush 期整理撞车也不会重复付出代价。
func (s *MTSStore) repairCompactionLocked(ctx context.Context) (bool, error) {
	if err := s.ensureOpen(); err != nil {
		return false, err
	}
	if err := s.engine.Flush(ctx); err != nil {
		return false, fmt.Errorf("flush MTS before compaction repair: %w", err)
	}
	if _, err := s.engine.CompactWithResult(ctx); err != nil {
		return false, fmt.Errorf("compact MTS during repair: %w", err)
	}
	return true, nil
}

func compactionDegraded(reasons []string) bool {
	for _, reason := range reasons {
		if strings.HasPrefix(reason, compactionDegradedReason) {
			return true
		}
	}
	return false
}
