package store

import (
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

func queuedRetentionPolicy(t *testing.T, catalog *MetricCatalogStore) *model.MetricDefinition {
	t.Helper()
	definition, err := catalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	policy := metricPolicyFrom(definition)
	days := 1
	policy.RetentionDays = &days
	change, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy))
	if err != nil {
		t.Fatal(err)
	}
	return change.Definition
}

func TestMetricRetentionJobsRecoverRetryAndDoNotCompleteNewerPolicy(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	definition := queuedRetentionPolicy(t, catalog)
	job, err := catalog.ClaimMetricRetention(t.Context())
	if err != nil || job == nil || job.RetentionDays != 1 {
		t.Fatal("policy did not queue retention")
	}
	if duplicate, err := catalog.ClaimMetricRetention(t.Context()); err != nil || duplicate != nil {
		t.Fatal("active retention was claimed twice")
	}
	if err := catalog.CompleteMetricRetention(t.Context(), model.MetricRetentionCompletion{Job: *job, Error: "failed", CompletedAt: model.NowUTC()}); err != nil {
		t.Fatal(err)
	}
	if retry, err := catalog.ClaimMetricRetention(t.Context()); err != nil || retry != nil {
		t.Fatal("failed retention did not back off")
	}
	policy := metricPolicyFrom(definition)
	policy.Label = "Revised label"
	if _, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.CompleteMetricRetention(t.Context(), model.MetricRetentionCompletion{Job: *job, CompletedAt: model.NowUTC()}); err != nil {
		t.Fatal(err)
	}
	newer, err := catalog.ClaimMetricRetention(t.Context())
	if err != nil || newer == nil || newer.Revision == job.Revision {
		t.Fatal("old completion erased newer retention work")
	}
	if err := catalog.RecoverMetricRetention(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := catalog.ClaimMetricRetention(t.Context())
	if err != nil || recovered == nil {
		t.Fatal("interrupted retention stayed stuck")
	}
	if err := catalog.CompleteMetricRetention(t.Context(), model.MetricRetentionCompletion{Job: *recovered, CompletedAt: model.NowUTC()}); err != nil {
		t.Fatal(err)
	}
	if pending, err := catalog.ClaimMetricRetention(t.Context()); err != nil || pending != nil {
		t.Fatal("successful retention stayed pending")
	}
}

func TestMetricRetentionUsesPerDefinitionCutoff(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	_ = queuedRetentionPolicy(t, catalog)
	metrics := metricQueryMTS(t)
	now := model.NowUTC().Truncate(time.Second)
	for _, age := range []time.Duration{48 * time.Hour, time.Hour} {
		if err := metrics.WriteMetric(t.Context(), "node", "cpu", 25, now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	job, err := catalog.ClaimMetricRetention(t.Context())
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err := metrics.ApplyMetricRetention(t.Context(), *job); err != nil {
		t.Fatal(err)
	}
	points, err := metrics.QueryMetrics(t.Context(), []string{"cpu"}, now.Add(-72*time.Hour), now, "node")
	if err != nil || len(points["cpu"]) != 1 {
		t.Fatal("retention cutoff did not remove only expired values")
	}
}

func TestMetricRetentionDoesNotRunWhenClaimWasNotPersisted(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	_ = queuedRetentionPolicy(t, catalog)
	installCustomMetricFault(t, sqlite.DB, "CREATE TRIGGER ignore_retention_claim BEFORE UPDATE ON metric_retention_jobs BEGIN SELECT RAISE(IGNORE); END")
	if job, err := catalog.ClaimMetricRetention(t.Context()); err == nil || job != nil {
		t.Fatal("unpersisted claim could delete history")
	}
}
