package store

import (
	"strings"
	"sync"
	"time"

	"github.com/beat/backend/internal/metricquery"
)

// metricQueryEdgeCacheTTL 是 available_from 边缘探测结果的存活时间。探测本身要合并整段历史，
// 而 available_from 是展示用的提示：同一个序列在一分钟内重复查询没必要重复付这份代价。
const metricQueryEdgeCacheTTL = 60 * time.Second

// metricQueryEdgeCacheEntries 限制缓存条目数，避免高基数实例把内存拖大。
const metricQueryEdgeCacheEntries = 4096

// metricQueryEdgeEntry 记录一次探测的结论：要么找到最早样本，要么（无数据/预算耗尽）没有结论。
type metricQueryEdgeEntry struct {
	expires time.Time
	found   bool
	point   TimePoint
}

// metricQueryEdgeCache 是每个 MTSStore 私有的小缓存，键为序列身份。
type metricQueryEdgeCache struct {
	mu      sync.Mutex
	entries map[string]metricQueryEdgeEntry
}

func (cache *metricQueryEdgeCache) get(key string, now time.Time) (metricQueryEdgeEntry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[key]
	if !ok || !now.Before(entry.expires) {
		return metricQueryEdgeEntry{}, false
	}
	return entry, true
}

func (cache *metricQueryEdgeCache) put(key string, entry metricQueryEdgeEntry, now time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[string]metricQueryEdgeEntry, metricQueryEdgeCacheEntries/4)
	}
	if len(cache.entries) >= metricQueryEdgeCacheEntries {
		cache.evict(now)
	}
	cache.entries[key] = entry
}

// evict 先清掉过期条目；仍然满员时整份丢弃，宁可重新探测也不让缓存无界增长。
func (cache *metricQueryEdgeCache) evict(now time.Time) {
	for key, entry := range cache.entries {
		if !now.Before(entry.expires) {
			delete(cache.entries, key)
		}
	}
	if len(cache.entries) >= metricQueryEdgeCacheEntries {
		cache.entries = make(map[string]metricQueryEdgeEntry, metricQueryEdgeCacheEntries/4)
	}
}

// metricQueryEdgeKey 用序列身份做键：区间与聚合方式不影响「最早样本」的结论。
func metricQueryEdgeKey(scan metricquery.Scan) string {
	parts := []string{scan.Metric, scan.NodeID, scan.DeviceID, scan.Vendor}
	return strings.Join(parts, "\x00")
}
