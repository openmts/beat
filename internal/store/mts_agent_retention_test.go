package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func TestAgentReceiptParticipatesInCatalogRecoveryAndRetention(t *testing.T) {
	metrics := metricQueryMTS(t)
	old := ingestInput(time.Now().Add(-48*time.Hour), 100)
	acceptIngest(t, metrics, old)
	sqlite := setupTestDB(t)
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err != nil {
		t.Fatal("typed receipt rejected by recovered inventory", err)
	}
	if err := metrics.CleanupBefore(t.Context(), time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	receipt, err := metrics.LatestAgentReceipt(t.Context(), "node")
	if err != nil || receipt != nil {
		t.Fatal("expired receipt escaped retention", err)
	}
	values, err := metrics.QueryLatest(t.Context(), "node")
	if err != nil || len(values) != 0 {
		t.Fatal("receipt and numeric retention diverged", err)
	}
}

func TestAgentReceiptSchemaCannotBecomeAQueryableCustomMetric(t *testing.T) {
	sqlite := setupTestDB(t)
	metrics := metricQueryMTS(t)
	if err := metrics.WriteMetric(t.Context(), "node", model.AgentReceiptMeasurement, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := metrics.AttachMetricCatalog(t.Context(), NewMetricCatalogStore(sqlite.DB)); err == nil {
		t.Fatal("scalar receipt schema was accepted")
	}
}
