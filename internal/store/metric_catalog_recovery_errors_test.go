package store

import (
	"context"
	"testing"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestMetricCatalogRecoveryRejectsFailedAndMalformedDefinitionReads(t *testing.T) {
	sqlite, _, metric, _ := customRegistrationFixture(t)
	for _, query := range []string{"SELECT * FROM missing_definitions", "SELECT NULL,NULL"} {
		fault := customMetricReadFault{schemaExecutor: sqlite.DB, rows: sqlite.DB, rowSQL: query}
		if err := reconcileCustomMetricDefinitions(t.Context(), fault); err == nil {
			t.Fatal("failed definition read ignored")
		}
	}
	if _, err := sqlite.DB.Exec("UPDATE custom_metrics SET name='invalid/name' WHERE id=?", metric.ID); err != nil {
		t.Fatal(err)
	}
	if err := NewMetricCatalogStore(sqlite.DB).Reconcile(t.Context()); err == nil {
		t.Fatal("invalid legacy definition admitted")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reconcileMetricCatalog(t.Context(), sqlite.DB); err == nil {
		t.Fatal("missing catalog state ignored")
	}
}

func TestMetricCatalogSelectorRejectsUnexpectedBuiltinAndPublicOverflow(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	definition := metriccatalog.Builtins()[0]
	definition.Key = "extra_builtin"
	if err := insertMetricDefinition(t.Context(), sqlite.DB, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.BuiltinPersistence(t.Context()); err == nil {
		t.Fatal("unexpected builtin admitted")
	}
	if err := validateStoredMetricSemantics(model.MetricDefinition{Source: "custom", Key: "custom_"}); err == nil {
		t.Fatal("empty custom name admitted")
	}
}

func TestMetricRetentionStateStorageErrorsDoNotInventWorkOrSuccess(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	_ = queuedRetentionPolicy(t, catalog)
	installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER reject_retention_claim BEFORE UPDATE ON metric_retention_jobs BEGIN SELECT RAISE(ABORT,'claim failed'); END")
	if _, err := catalog.ClaimMetricRetention(t.Context()); err == nil {
		t.Fatal("failed claim returned work")
	}
	installCustomMetricFault(t, sqlite.DB, "DROP TRIGGER reject_retention_claim", "ALTER TABLE metric_retention_jobs RENAME COLUMN state TO invalid_state")
	if _, err := catalog.ClaimMetricRetention(t.Context()); err == nil {
		t.Fatal("claim query failure became empty queue")
	}
	if err := catalog.RecoverMetricRetention(t.Context()); err == nil {
		t.Fatal("recovery error ignored")
	}
	if err := catalog.CompleteMetricRetention(t.Context(), model.MetricRetentionCompletion{CompletedAt: model.NowUTC()}); err == nil {
		t.Fatal("completion error ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.ClaimMetricRetention(ctx); err == nil {
		t.Fatal("canceled claim ran")
	}
}

func TestMetricInventoryAttachmentAndCleanupPropagateCatalogFailure(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	metrics := metricQueryMTS(t)
	if err := metrics.AttachMetricCatalog(t.Context(), catalog); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := metrics.CleanupBefore(t.Context(), model.NowUTC()); err == nil {
		t.Fatal("cleanup ignored missing catalog")
	}
	if _, err := metrics.ExportRows(t.Context(), model.NowUTC(), func(_ mts.Row) error { return nil }); err == nil {
		t.Fatal("export ignored missing catalog")
	}
}
