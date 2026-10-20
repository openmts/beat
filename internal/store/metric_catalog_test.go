package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestMetricCatalogSeedsPublicBuiltinsAndBackfillsCustomDefinitions(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	expected := 0
	for _, definition := range metriccatalog.Builtins() {
		if definition.Public {
			expected++
		}
	}
	before, err := catalog.Catalog(t.Context(), true)
	if err != nil || before.Version != metriccatalog.Version || len(before.Metrics) != expected {
		t.Fatalf("catalog seed: %v, %v", before, err)
	}
	custom, err := NewCustomMetricStore(sqlite.DB).Create(t.Context(), model.CustomMetric{Name: "queue", ShellCommand: "printf 1", IntervalSeconds: 5, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := catalog.Get(t.Context(), model.CustomMetricPrefix+custom.Name)
	if err != nil || definition == nil || definition.Unit != "number" {
		t.Fatalf("custom inventory: %v / %v", definition, err)
	}
	after, err := catalog.Catalog(t.Context(), true)
	if err != nil || after.Revision <= before.Revision {
		t.Fatal("custom registration did not advance catalog revision")
	}
	encoded, err := json.Marshal(after)
	if err != nil || strings.Contains(string(encoded), "printf") {
		t.Fatal("public catalog exposed a collection command")
	}
	if err := NewCustomMetricStore(sqlite.DB).Delete(t.Context(), custom.ID); err != nil {
		t.Fatal(err)
	}
	definition, err = catalog.Get(t.Context(), model.CustomMetricPrefix+custom.Name)
	if err != nil || definition == nil {
		t.Fatal("deletion lost the historical metric inventory")
	}
}

func TestMetricCatalogReopenRejectsSemanticDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	sqlite, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("UPDATE metric_definitions SET unit='seconds' WHERE metric_key='cpu'"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path)
	if reopened != nil {
		_ = reopened.Close()
	}
	if err == nil {
		t.Fatal("startup silently changed the unit of existing measurements")
	}
}

func metricPolicyFrom(definition *model.MetricDefinition) model.MetricPolicyUpdate {
	return model.MetricPolicyUpdate{Revision: definition.Revision, Label: definition.Label, Description: definition.Description,
		DefaultAggregation: definition.DefaultAggregation, Enabled: definition.Enabled, Public: definition.Public,
		RetentionDays: definition.RetentionOverride}
}

func catalogPolicyWrite(t *testing.T, catalog *MetricCatalogStore, policy model.MetricPolicyUpdate) MetricCatalogPolicyChange {
	t.Helper()
	admins := NewAdminStore(catalog.db)
	user, err := admins.GetUserByUsername(t.Context(), "catalog-owner")
	if err != nil {
		t.Fatal(err)
	}
	if user == nil {
		value := testAdminUser("catalog-owner", model.AdminRoleOwner)
		if err := admins.CreateUser(t.Context(), &value); err != nil {
			t.Fatal(err)
		}
		user = &value
		now := model.NowUTC()
		until := now.Add(time.Hour)
		if err := admins.CreateSession(t.Context(), &model.AdminSession{ID: "catalog-session", UserID: user.ID, TokenHash: []byte("catalog-test"), CreatedAt: now, LastActivityAt: now, IdleExpiresAt: until, AbsoluteExpiresAt: until, ReauthenticatedUntil: &until}); err != nil {
			t.Fatal(err)
		}
	}
	return MetricCatalogPolicyChange{Policy: policy, SessionID: "catalog-session", Audit: model.AdminAuditEvent{ActorID: user.ID}}
}

func TestMetricCatalogPolicyUsesRevisionAndAtomicAudit(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	definition, err := catalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	policy := metricPolicyFrom(definition)
	policy.Label, policy.Public = "Private CPU", false
	change, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy))
	if err != nil || change.Definition.Revision != definition.Revision+1 {
		t.Fatalf("policy update: %v", err)
	}
	if _, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy)); !errors.Is(err, ErrMetricCatalogConflict) {
		t.Fatal("stale policy overwrote current revision")
	}
	public, err := catalog.Catalog(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range public.Metrics {
		if metric.Key == "cpu" {
			t.Fatal("private metric remained public")
		}
	}
	installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER catalog_audit_failure BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT,'audit unavailable'); END")
	policy = metricPolicyFrom(change.Definition)
	policy.Label = "Lost update"
	if _, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy)); err == nil {
		t.Fatal("audit failure was ignored")
	}
	stored, err := catalog.Get(t.Context(), "cpu")
	if err != nil || stored.Label != change.Definition.Label || stored.Revision != change.Definition.Revision {
		t.Fatal("audit failure committed policy")
	}
}

func TestMetricCatalogDisableQueuesStopAndAcknowledgesCachedCustomValues(t *testing.T) {
	sqlite, metrics, metric, nodes := customRegistrationFixture(t)
	if err := metrics.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
		t.Fatal(err)
	}
	catalog := NewMetricCatalogStore(sqlite.DB)
	definition, err := catalog.Get(t.Context(), model.CustomMetricPrefix+metric.Name)
	if err != nil || definition == nil {
		t.Fatalf("custom catalog definition: %v", err)
	}
	policy := metricPolicyFrom(definition)
	policy.Enabled = false
	change, err := catalog.UpdatePolicy(t.Context(), definition.Key, catalogPolicyWrite(t, catalog, policy))
	if err != nil || len(change.Tasks) != 1 || !strings.Contains(change.Tasks[0].Payload, `"metrics":[]`) {
		t.Fatalf("disable did not queue stop: %v", err)
	}
	accepted, err := catalog.ValidateCustomBatch(t.Context(), nodes[0], map[string]float64{metric.Name: 3})
	if err != nil || len(accepted) != 0 {
		t.Fatal("cached disabled sample blocked heartbeat or remained writable")
	}
	if _, err := catalog.ValidateCustomBatch(t.Context(), nodes[1], map[string]float64{metric.Name: 3}); err == nil {
		t.Fatal("disabled policy accepted an unbound node")
	}
}
