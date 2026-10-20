package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

func TestMetricCatalogControlsPublicAndPersistentBuiltinValues(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	public, err := catalog.PublicKeys(t.Context())
	if err != nil || !public["cpu"] {
		t.Fatal("builtin is missing")
	}
	definition, err := catalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	policy := metricPolicyFrom(definition)
	policy.Public, policy.Enabled = false, false
	if _, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy)); err != nil {
		t.Fatal(err)
	}
	public, err = catalog.PublicKeys(t.Context())
	if err != nil || public["cpu"] {
		t.Fatal("private/disabled metric stayed public")
	}
	enabled, err := catalog.BuiltinPersistence(t.Context())
	if err != nil || enabled["cpu"] || !enabled["memory"] {
		t.Fatal("persistence policy did not apply")
	}
	if _, err := sqlite.DB.Exec("DELETE FROM metric_definitions WHERE metric_key='memory'"); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.BuiltinPersistence(t.Context()); err == nil {
		t.Fatal("incomplete builtin inventory accepted")
	}
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.PublicKeys(t.Context()); err == nil {
		t.Fatal("closed public catalog returned success")
	}
	if _, err := catalog.BuiltinPersistence(t.Context()); err == nil {
		t.Fatal("closed persistence catalog returned success")
	}
}

func TestMetricCatalogValidatesWholeCustomBatchAndDisabledTransition(t *testing.T) {
	sqlite, customs, metric, nodes := customRegistrationFixture(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	if err := customs.SetMetricNodes(t.Context(), metric.ID, nodes[:1]); err != nil {
		t.Fatal(err)
	}
	values, err := catalog.ValidateCustomBatch(t.Context(), nodes[0], map[string]float64{metric.Name: -2})
	if err != nil || len(values) != 1 || values[0].Name != model.CustomMetricPrefix+metric.Name {
		t.Fatal("registered finite custom value rejected")
	}
	for _, batch := range []map[string]float64{{"unknown": 1}, {metric.Name: math.NaN()}, {"bad name": 1}} {
		if _, err := catalog.ValidateCustomBatch(t.Context(), nodes[0], batch); !errors.Is(err, ErrMetricIngestion) {
			t.Fatal("invalid custom batch accepted")
		}
	}
	large := make(map[string]float64)
	for i := range maxCustomMetricDefinitionsPerNode + 1 {
		large[fmt.Sprintf("metric_%d", i)] = 1
	}
	if _, err := catalog.ValidateCustomBatch(t.Context(), nodes[0], large); !errors.Is(err, ErrMetricIngestion) {
		t.Fatal("oversized batch accepted")
	}
	if _, err := catalog.ValidateCustomBatch(t.Context(), nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.DB.Exec("UPDATE custom_metrics SET enabled=0 WHERE id=?", metric.ID); err != nil {
		t.Fatal(err)
	}
	values, err = catalog.ValidateCustomBatch(t.Context(), nodes[0], map[string]float64{metric.Name: 1})
	if err != nil || len(values) != 0 {
		t.Fatal("disabled cached sample poisoned heartbeat")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := catalog.ValidateCustomBatch(ctx, nodes[0], map[string]float64{metric.Name: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal("binding query cancellation ignored")
	}
}

func TestMetricReportWritesEnabledValuesAndCustomTogether(t *testing.T) {
	metrics := metricQueryMTS(t)
	now := model.NowUTC().Truncate(time.Second)
	enabled := make(map[string]bool)
	for _, definition := range metriccatalog.Builtins() {
		enabled[definition.Key] = true
	}
	enabled["cpu"] = false
	sample := MetricReportSample{NodeID: "node", Metrics: &model.NodeMetrics{CPU: 25, Memory: 50, NetRecvTotal: 100, NetSentTotal: 200},
		Custom: []model.MetricValue{{Name: "custom_queue", Value: 7}}, EnabledBuiltins: enabled, Timestamp: now}
	if err := metrics.WriteMetricReport(t.Context(), sample); err != nil {
		t.Fatal(err)
	}
	values, err := metrics.QueryMetrics(t.Context(), []string{"cpu", "memory", "custom_queue"}, now.Add(-time.Second), now.Add(time.Second), "node")
	if err != nil || len(values["cpu"]) != 0 || len(values["memory"]) != 1 || len(values["custom_queue"]) != 1 {
		t.Fatal("report ignored persistence policy")
	}
	sample.Timestamp = now.Add(time.Second)
	sample.EnabledBuiltins = nil
	sample.Metrics.NetRecvTotal = 150
	if err := metrics.WriteMetricReport(t.Context(), sample); err != nil {
		t.Fatal(err)
	}
	sample.Metrics = nil
	sample.Custom = nil
	if err := metrics.WriteMetricReport(t.Context(), sample); err != nil {
		t.Fatal(err)
	}
	sample.Metrics = &model.NodeMetrics{CPU: 101}
	if err := metrics.WriteMetricReport(t.Context(), sample); err == nil {
		t.Fatal("invalid builtin value accepted")
	}
	sample.Metrics = &model.NodeMetrics{}
	sample.EnabledBuiltins = map[string]bool{"net_recv_delta": true}
	if err := metrics.WriteMetricReport(t.Context(), sample); err == nil {
		t.Fatal("derived traffic without source counter accepted")
	}
	sample.EnabledBuiltins = map[string]bool{}
	if err := metrics.WriteMetricReport(t.Context(), sample); err != nil {
		t.Fatal("disabled report should succeed without points")
	}
	if err := metrics.Close(); err != nil {
		t.Fatal(err)
	}
	if err := metrics.WriteMetricReport(t.Context(), sample); !errors.Is(err, ErrMTSStoreClosed) {
		t.Fatal("closed report returned success")
	}
}
