package store

import (
	"context"
	"errors"
	"testing"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestMetricCatalogCapacityRejectsNewNamesAndOversizedLegacyInventory(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	now := model.NowUTC()
	_, err := sqlite.DB.Exec(`WITH RECURSIVE seq(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM seq WHERE n<?)
		INSERT INTO metric_definitions(metric_key,label,description,source,kind,unit,aggregations_json,default_aggregation,
		public,enabled,revision,tags_json,created_at,updated_at)
		SELECT 'custom_capacity_'||n,'Capacity '||n,'','custom','gauge','number','["avg","min","max","sum","last"]','avg',
		0,1,1,'["node"]',?,? FROM seq`, metriccatalog.MaximumDefinitions-len(metriccatalog.Builtins()), now, now)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := metriccatalog.Custom("overflow", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.RegisterHistory(t.Context(), []model.MetricDefinition{definition}); err == nil {
		t.Fatal("catalog admitted unbounded new names")
	}
	if err := catalog.RegisterHistory(t.Context(), make([]model.MetricDefinition, metriccatalog.MaximumDefinitions+1)); err == nil {
		t.Fatal("unbounded reconciliation batch accepted")
	}
	if err := insertMetricDefinition(t.Context(), sqlite.DB, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Catalog(t.Context(), false); err == nil {
		t.Fatal("oversized legacy catalog returned unbounded data")
	}
}

func TestMetricCatalogRejectsMalformedPersistedJSONAndUnknownSemantics(t *testing.T) {
	for _, column := range []string{"aggregations_json", "tags_json"} {
		t.Run(column, func(t *testing.T) {
			sqlite := setupTestDB(t)
			sqlite.DB.SetMaxOpenConns(1)
			installCustomMetricFault(t, sqlite.DB, "PRAGMA ignore_check_constraints=ON", "UPDATE metric_definitions SET "+column+"='malformed' WHERE metric_key='cpu'")
			if _, err := NewMetricCatalogStore(sqlite.DB).Get(t.Context(), "cpu"); err == nil {
				t.Fatal("malformed catalog JSON accepted")
			}
		})
	}
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	definition, err := metriccatalog.Custom("drift", "")
	if err != nil {
		t.Fatal(err)
	}
	definition.Minimum = new(float64)
	if err := catalog.RegisterHistory(t.Context(), []model.MetricDefinition{definition}); err == nil {
		t.Fatal("custom semantic drift accepted")
	}
	definition.Unit = "invalid"
	if err := catalog.RegisterHistory(t.Context(), []model.MetricDefinition{definition}); err == nil {
		t.Fatal("invalid custom unit persisted")
	}
}

func TestMetricPolicyMissingInvalidAndCanceledRequestsDoNotMutate(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	definition, err := catalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	change := catalogPolicyWrite(t, catalog, metricPolicyFrom(definition))
	if _, err := catalog.UpdatePolicy(t.Context(), "missing", change); !errors.Is(err, ErrMetricCatalogNotFound) {
		t.Fatal("missing metric policy accepted")
	}
	change.Policy.Label = ""
	if _, err := catalog.UpdatePolicy(t.Context(), "cpu", change); !errors.Is(err, metriccatalog.ErrInvalidDefinition) {
		t.Fatal("invalid policy accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.UpdatePolicy(ctx, "cpu", change); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled mutation accepted")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := applyMetricCatalog(t.Context(), sqlite.DB); err == nil {
		t.Fatal("schema error ignored")
	}
	if err := authorizeMetricPolicy(t.Context(), sqlite.DB, &change); err == nil {
		t.Fatal("authority storage error ignored")
	}
	if err := catalog.RegisterHistory(t.Context(), nil); err == nil {
		t.Fatal("closed reconciliation succeeded")
	}
}

func TestMetricPolicyTaskQueueFailureRollsBackEnableState(t *testing.T) {
	sqlite, customs, metric, nodes := customRegistrationFixture(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	if err := customs.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.Get(t.Context(), model.CustomMetricPrefix+metric.Name)
	if err != nil {
		t.Fatal(err)
	}
	policy := metricPolicyFrom(definition)
	policy.Enabled = false
	change := catalogPolicyWrite(t, catalog, policy)
	installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER catalog_task_failure BEFORE INSERT ON agent_tasks BEGIN SELECT RAISE(ABORT,'queue failed'); END")
	if _, err := catalog.UpdatePolicy(t.Context(), definition.Key, change); err == nil {
		t.Fatal("stop task failure was acknowledged")
	}
	after, err := catalog.Get(t.Context(), definition.Key)
	if err != nil || !after.Enabled || after.Revision != definition.Revision {
		t.Fatal("failed stop left policy disabled")
	}
}
