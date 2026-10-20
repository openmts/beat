package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/beat/backend/internal/metricquery"
)

func TestMetricQueryEdgeCacheBoundsItsEntries(t *testing.T) {
	cache := &metricQueryEdgeCache{}
	now := time.Now().UTC()
	if _, ok := cache.get("absent", now); ok {
		t.Fatal("empty cache reported a hit")
	}
	cache.put("fresh", metricQueryEdgeEntry{expires: now.Add(time.Minute), found: true, point: TimePoint{Value: 1}}, now)
	entry, ok := cache.get("fresh", now)
	if !ok || !entry.found || entry.point.Value != 1 {
		t.Fatalf("cache entry = %#v/%v", entry, ok)
	}
	if _, ok := cache.get("fresh", now.Add(2*time.Minute)); ok {
		t.Fatal("expired entry reported a hit")
	}
	for index := 0; index < metricQueryEdgeCacheEntries*2; index++ {
		cache.put(fmt.Sprintf("live-%d", index), metricQueryEdgeEntry{expires: now.Add(time.Minute)}, now)
		if len(cache.entries) > metricQueryEdgeCacheEntries {
			t.Fatalf("cache grew past its bound: %d", len(cache.entries))
		}
	}
	newest := fmt.Sprintf("live-%d", metricQueryEdgeCacheEntries*2-1)
	if _, ok := cache.get(newest, now); !ok {
		t.Fatal("newest entry was dropped")
	}
	for index := 0; index < metricQueryEdgeCacheEntries; index++ {
		cache.put(fmt.Sprintf("stale-%d", index), metricQueryEdgeEntry{expires: now.Add(-time.Minute)}, now)
	}
	cache.put("replacement", metricQueryEdgeEntry{expires: now.Add(time.Minute)}, now)
	if len(cache.entries) >= metricQueryEdgeCacheEntries {
		t.Fatalf("saturated cache kept %d entries", len(cache.entries))
	}
	if _, ok := cache.get("replacement", now); !ok {
		t.Fatal("replacement entry was dropped")
	}
	if _, ok := cache.get("stale-0", now); ok {
		t.Fatal("oldest expired entry survived eviction")
	}
}

func TestMetricQueryEdgeKeySeparatesSeriesIdentity(t *testing.T) {
	first := metricQueryEdgeKey(metricquery.Scan{Metric: "cpu", NodeID: "node-a", DeviceID: "0", Vendor: "acme"})
	same := metricQueryEdgeKey(metricquery.Scan{Metric: "cpu", NodeID: "node-a", DeviceID: "0", Vendor: "acme"})
	other := metricQueryEdgeKey(metricquery.Scan{Metric: "cpu", NodeID: "node-b", DeviceID: "0", Vendor: "acme"})
	if first != same {
		t.Fatal("identical series must share a cache key")
	}
	if first == other {
		t.Fatal("different nodes must not share a cache key")
	}
}
