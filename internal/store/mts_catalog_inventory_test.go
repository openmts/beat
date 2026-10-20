package store

import (
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/model"
)

func TestMTSCatalogInventoryExportsAndRetainsDeletedCustomHistory(t *testing.T) {
	sqlite := setupTestDB(t)
	metrics := metricQueryMTS(t)
	customs := NewCustomMetricStore(sqlite.DB)
	custom, err := customs.Create(t.Context(), model.CustomMetric{Name: "historic", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, sample := range []struct {
		age   time.Duration
		value float64
	}{{48 * time.Hour, 11}, {time.Hour, 12}} {
		if err := metrics.WriteMetric(t.Context(), "node", "custom_historic", sample.value, now.Add(-sample.age)); err != nil {
			t.Fatal(err)
		}
	}
	if err := customs.Delete(t.Context(), custom.ID); err != nil {
		t.Fatal(err)
	}
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err != nil {
		t.Fatal(err)
	}
	var exported int
	if _, err := metrics.ExportRows(t.Context(), now, func(row mts.Row) error {
		if row.Measurement == "custom_historic" {
			exported++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if exported != 2 {
		t.Fatalf("custom history export count=%d", exported)
	}
	if err := metrics.CleanupBefore(t.Context(), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	points, err := metrics.QueryMetrics(t.Context(), []string{"custom_historic"}, now.Add(-72*time.Hour), now, "node")
	if err != nil || len(points["custom_historic"]) != 1 || points["custom_historic"][0].Value != 12 {
		t.Fatal("custom retention omitted catalog history")
	}
}

func TestMTSCatalogInventoryRecoversPreCatalogCustomMeasurementAsPrivate(t *testing.T) {
	sqlite := setupTestDB(t)
	metrics := metricQueryMTS(t)
	if err := metrics.WriteMetric(t.Context(), "old-node", "custom_retired", 3, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	catalog := NewMetricCatalogStore(sqlite.DB)
	if err := metrics.AttachMetricCatalog(t.Context(), catalog); err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.Get(t.Context(), "custom_retired")
	if err != nil || definition == nil || definition.Public {
		t.Fatal("old custom measurement was lost or automatically published")
	}
	if err := metrics.AttachMetricCatalog(t.Context(), catalog); err != nil {
		t.Fatal("inventory attachment is not idempotent")
	}
}
