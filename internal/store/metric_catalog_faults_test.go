package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestMetricCatalogReconcileBackfillsExistingDefinitionsAndPreservesPolicy(t *testing.T) {
	sqlite, customs, metric, _ := customRegistrationFixture(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	if _, err := sqlite.DB.Exec("DELETE FROM metric_definitions WHERE metric_key=?", model.CustomMetricPrefix+metric.Name); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.Get(t.Context(), model.CustomMetricPrefix+metric.Name)
	if err != nil || definition == nil {
		t.Fatal("legacy custom definition was lost")
	}
	policy := metricPolicyFrom(definition)
	policy.Public = false
	if _, err := catalog.UpdatePolicy(t.Context(), definition.Key, catalogPolicyWrite(t, catalog, policy)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	definition, err = catalog.Get(t.Context(), definition.Key)
	if err != nil || definition.Public {
		t.Fatal("reconcile overwrote owner policy")
	}
	if err := customs.Delete(t.Context(), metric.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.UpdatePolicy(t.Context(), definition.Key, catalogPolicyWrite(t, catalog, metricPolicyFrom(definition))); err != nil {
		t.Fatal("retired history cannot be governed")
	}
	bounded, err := customCatalogDefinition("bounded", strings.Repeat("文", 1000))
	if err != nil || len(bounded.Description) > metriccatalog.MaximumDescriptionBytes {
		t.Fatal("legacy description backfill was unbounded")
	}
}

func TestMetricCatalogStorageFailuresNeverReturnSuccessfulEmptyCatalog(t *testing.T) {
	for _, statement := range []string{
		"UPDATE metric_catalog_state SET version=999",
		"ALTER TABLE metric_definitions RENAME COLUMN label TO unavailable_label",
		"UPDATE metric_definitions SET tags_json='{}' WHERE metric_key='cpu'",
		"UPDATE metric_definitions SET source='custom' WHERE metric_key='cpu'",
	} {
		t.Run(statement, func(t *testing.T) {
			sqlite := setupTestDB(t)
			catalog := NewMetricCatalogStore(sqlite.DB)
			installCustomMetricFault(t, sqlite.DB, statement)
			if _, err := catalog.Catalog(t.Context(), false); err == nil {
				t.Fatal("corrupt catalog returned success")
			}
			if err := catalog.Reconcile(t.Context()); err == nil {
				t.Fatal("startup ignored corrupt catalog")
			}
		})
	}
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.Catalog(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled catalog read succeeded")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Catalog(t.Context(), false); err == nil {
		t.Fatal("closed catalog returned success")
	}
}

func TestMetricCatalogPolicyRollsBackQueueAndCommitFailures(t *testing.T) {
	for _, failure := range []string{"update", "ignored", "job", "commit"} {
		t.Run(failure, func(t *testing.T) {
			sqlite := setupTestDB(t)
			catalog := NewMetricCatalogStore(sqlite.DB)
			definition, err := catalog.Get(t.Context(), "cpu")
			if err != nil {
				t.Fatal(err)
			}
			policy := metricPolicyFrom(definition)
			policy.Label = "Must roll back"
			change := catalogPolicyWrite(t, catalog, policy)
			switch failure {
			case "update":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER catalog_failure BEFORE UPDATE ON metric_definitions BEGIN SELECT RAISE(ABORT,'write failed'); END")
			case "ignored":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER catalog_failure BEFORE UPDATE ON metric_definitions BEGIN SELECT RAISE(IGNORE); END")
			case "job":
				installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER catalog_failure BEFORE INSERT ON metric_retention_jobs BEGIN SELECT RAISE(ABORT,'job failed'); END")
			case "commit":
				installCustomMetricCommitFault(t, sqlite.DB, "INSERT ON metric_retention_jobs")
			}
			if _, err := catalog.UpdatePolicy(t.Context(), "cpu", change); err == nil {
				t.Fatal("failed policy mutation was acknowledged")
			}
			after, err := catalog.Get(t.Context(), "cpu")
			if err != nil || after.Label != definition.Label || after.Revision != definition.Revision {
				t.Fatal("failed mutation persisted")
			}
		})
	}
}

func TestMetricCatalogTrafficDependenciesPreventPoisoningReports(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	for _, pair := range [][2]string{{"net_recv_total", "net_recv_delta"}, {"net_sent_total", "net_sent_delta"}} {
		counter, err := catalog.Get(t.Context(), pair[0])
		if err != nil {
			t.Fatal(err)
		}
		policy := metricPolicyFrom(counter)
		policy.Enabled = false
		if _, err := catalog.UpdatePolicy(t.Context(), counter.Key, catalogPolicyWrite(t, catalog, policy)); !errors.Is(err, metriccatalog.ErrInvalidDefinition) {
			t.Fatal("source disabled under active derived metric")
		}
		delta, err := catalog.Get(t.Context(), pair[1])
		if err != nil {
			t.Fatal(err)
		}
		deltaPolicy := metricPolicyFrom(delta)
		deltaPolicy.Enabled = false
		changed, err := catalog.UpdatePolicy(t.Context(), delta.Key, catalogPolicyWrite(t, catalog, deltaPolicy))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catalog.UpdatePolicy(t.Context(), counter.Key, catalogPolicyWrite(t, catalog, policy)); err != nil {
			t.Fatal(err)
		}
		deltaPolicy = metricPolicyFrom(changed.Definition)
		deltaPolicy.Enabled = true
		if _, err := catalog.UpdatePolicy(t.Context(), delta.Key, catalogPolicyWrite(t, catalog, deltaPolicy)); !errors.Is(err, metriccatalog.ErrInvalidDefinition) {
			t.Fatal("derived metric enabled without source")
		}
	}
}
