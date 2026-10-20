package store

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

// 自定义指标的名称由用户决定、只登记在指标目录里；擦除清单漏掉它们会让任何含自定义
// 指标的节点永久无法删除历史（实测表现为 inventory_mismatch 并让 worker 反复失败）。
func TestMetricErasureInventoryIncludesRegisteredCustomMeasurement(t *testing.T) {
	sqlite := setupTestDB(t)
	metrics := metricQueryMTS(t)
	at := time.Unix(1_700_300_000, 0).UTC()
	if err := metrics.WriteMetric(t.Context(), "node-1", "custom_uptime_seconds", 7, at); err != nil {
		t.Fatal(err)
	}
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err != nil {
		t.Fatal(err)
	}
	session, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureDeleteNode, "node-1", "")
	if err != nil {
		t.Fatalf("delete-node erasure rejected a registered custom measurement: %v", err)
	}
	if plan := session.Plan().Measurements; !slices.Contains(plan, "custom_uptime_seconds") {
		t.Fatalf("erasure plan omitted the custom measurement: %v", plan)
	}
	if err := session.Erase(t.Context(), "custom_uptime_seconds"); err != nil {
		t.Fatalf("erase custom measurement: %v", err)
	}
	session.Close()
	if err := metrics.CompactMetricErasure(t.Context()); err != nil {
		t.Fatalf("compact after custom erasure: %v", err)
	}
	points, err := metrics.QueryMetrics(t.Context(), []string{"custom_uptime_seconds"},
		at.Add(-time.Minute), at.Add(time.Minute), "node-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(points["custom_uptime_seconds"]) != 0 {
		t.Fatalf("custom history survived erasure: %v", points["custom_uptime_seconds"])
	}
}

// 目录存在也不能放宽封闭性：未登记的测量必须继续失败关闭。
func TestMetricErasureWithCatalogStillRejectsUnregisteredMeasurement(t *testing.T) {
	sqlite := setupTestDB(t)
	metrics := metricQueryMTS(t)
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err != nil {
		t.Fatal(err)
	}
	points := []mts.Point{{Measurement: "rogue_measurement", Tags: map[string]string{"node": "node-1"},
		Fields: map[string]mts.FieldValue{"value": mts.Float64Value(1)}, Timestamp: time.Now().Unix(),
		Precision: mts.PrecisionSecond}}
	if err := metrics.engine.Write(t.Context(), points, mts.WriteOptions{}); err != nil {
		t.Fatalf("write rogue measurement: %v", err)
	}
	if _, err := metrics.BeginMetricErasure(t.Context(), model.MetricErasureNodeHistory, "node-1", ""); !errors.Is(err, ErrMetricErasureInventory) {
		t.Fatalf("unregistered measurement error = %v", err)
	}
}
