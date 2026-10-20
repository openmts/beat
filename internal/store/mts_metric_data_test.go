package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/openmts/mts"

	"github.com/beat/backend/internal/metricquery"
	"github.com/beat/backend/internal/model"
)

func TestMTSQueryRejectsWrongStoredTypeNegativeCounterAndAggregateOverflow(t *testing.T) {
	for _, scenario := range []string{"wrong type", "negative counter", "sum overflow"} {
		t.Run(scenario, func(t *testing.T) {
			metrics := metricQueryMTS(t)
			from := model.NowUTC().Truncate(time.Second)
			plan := metricquery.Scan{NodeID: "node", Metric: "custom_value", Aggregation: "sum", From: from, To: from.Add(time.Minute), Buckets: 1, Interval: time.Minute}
			switch scenario {
			case "wrong type":
				if err := metrics.ImportPoints(t.Context(), []mts.Point{{Measurement: plan.Metric, Tags: map[string]string{"node": "node"}, Fields: map[string]mts.FieldValue{"value": mts.Int64Value(1)}, Timestamp: from.UnixNano()}}); err != nil {
					t.Fatal(err)
				}
			case "negative counter":
				plan.Aggregation = "rate"
				if err := metrics.WriteMetric(t.Context(), "node", plan.Metric, -1, from.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
			case "sum overflow":
				for _, offset := range []time.Duration{0, time.Second} {
					if err := metrics.WriteMetric(t.Context(), "node", plan.Metric, math.MaxFloat64, from.Add(offset)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := metrics.ReadMetricQuery(t.Context(), plan); err == nil {
				t.Fatal("invalid stored result returned successful chart")
			}
			if scenario == "wrong type" {
				if _, err := metrics.QueryMetrics(t.Context(), []string{plan.Metric}, from, plan.To, "node"); !errors.Is(err, metricquery.ErrArithmetic) {
					t.Fatal("raw reader accepted wrong stored type")
				}
			}
		})
	}
}

func TestMTSQueryRateWithoutPreviousObservationIsEmpty(t *testing.T) {
	metrics := metricQueryMTS(t)
	from := model.NowUTC().Truncate(time.Second)
	plan := metricquery.Scan{NodeID: "node", Metric: "net_recv_total", Aggregation: "rate", From: from, To: from.Add(time.Minute), Buckets: 1, Interval: time.Minute}
	values, err := metrics.ReadMetricQuery(t.Context(), plan)
	if err != nil || len(values.Points) != 1 || values.Points[0].Value != nil {
		t.Fatal("empty counter invented rate")
	}
}

func TestMTSOperationWaitSucceedsAfterExclusiveWorkCompletes(t *testing.T) {
	metrics := metricQueryMTS(t)
	metrics.operationMu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		unlock, err := metrics.lockMetricOperation(ctx, true)
		if err == nil {
			unlock()
		}
		done <- err
	}()
	time.Sleep(2 * metricOperationPoll)
	metrics.operationMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMetricRetentionOverrideIsIndependentOfGlobalCutoff(t *testing.T) {
	sqlite := setupTestDB(t)
	catalog := NewMetricCatalogStore(sqlite.DB)
	metrics := metricQueryMTS(t)
	definition, err := catalog.Get(t.Context(), "cpu")
	if err != nil {
		t.Fatal(err)
	}
	policy := metricPolicyFrom(definition)
	days := 7
	policy.RetentionDays = &days
	if _, err := catalog.UpdatePolicy(t.Context(), "cpu", catalogPolicyWrite(t, catalog, policy)); err != nil {
		t.Fatal(err)
	}
	if err := metrics.AttachMetricCatalog(t.Context(), catalog); err != nil {
		t.Fatal(err)
	}
	now := model.NowUTC()
	if err := metrics.WriteMetric(t.Context(), "node", "cpu", 10, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := metrics.CleanupBefore(t.Context(), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	points, err := metrics.QueryMetrics(t.Context(), []string{"cpu"}, now.Add(-72*time.Hour), now, "node")
	if err != nil || len(points["cpu"]) != 1 {
		t.Fatal("global cutoff overrode longer per-metric retention")
	}
}
