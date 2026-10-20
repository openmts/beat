package metricquery

import (
	"errors"
	"math"
	"testing"
	"time"
)

func aggregatePlan(aggregation string) Scan {
	from := time.Unix(1_700_000_000, 0).UTC()
	return Scan{NodeID: "node", Metric: "cpu", Aggregation: aggregation, From: from, To: from.Add(time.Minute), Interval: 20 * time.Second, Buckets: 3}
}

func TestMetricAggregationPreservesNullGapsAndRequestedDomain(t *testing.T) {
	for aggregation, expected := range map[string]float64{"avg": 15, "min": 10, "max": 20, "sum": 30, "last": 20} {
		t.Run(aggregation, func(t *testing.T) {
			plan := aggregatePlan(aggregation)
			buckets := NewBuckets(plan)
			for _, sample := range []struct {
				offset time.Duration
				value  float64
			}{{time.Second, 10}, {10 * time.Second, 20}, {50 * time.Second, 0}, {time.Minute, 999}} {
				if err := buckets.Add(plan.From.Add(sample.offset), sample.value); err != nil {
					t.Fatal(err)
				}
			}
			points, err := buckets.Points()
			if err != nil {
				t.Fatal(err)
			}
			if len(points) != 3 || points[0].Timestamp != plan.From.UnixMilli() || points[2].Timestamp != plan.From.Add(40*time.Second).UnixMilli() {
				t.Fatal("bucket domain drifted")
			}
			if points[0].Value == nil || *points[0].Value != expected || points[1].Value != nil || points[2].Value == nil || *points[2].Value != 0 {
				t.Fatalf("aggregation/gaps = %v", points)
			}
		})
	}
}

func TestMetricCounterRateHandlesResetAndMissingPreviousValue(t *testing.T) {
	plan := aggregatePlan("rate")
	buckets := NewBuckets(plan)
	for _, sample := range []struct {
		offset time.Duration
		value  float64
	}{{-time.Second, 100}, {time.Second, 120}, {11 * time.Second, 5}, {51 * time.Second, 85}} {
		if err := buckets.Add(plan.From.Add(sample.offset), sample.value); err != nil {
			t.Fatal(err)
		}
	}
	points, err := buckets.Points()
	if err != nil {
		t.Fatal(err)
	}
	if points[0].Value == nil || math.Abs(*points[0].Value-25.0/12) > 1e-12 || points[1].Value != nil || points[2].Value == nil || *points[2].Value != 2 {
		t.Fatal("counter reset or gap changed rate")
	}
	empty := NewBuckets(plan)
	if err := empty.Add(plan.From, 10); err != nil {
		t.Fatal(err)
	}
	points, err = empty.Points()
	if err != nil || points[0].Value != nil {
		t.Fatal("counter without a previous observation invented a rate")
	}
}

func TestMetricAggregationRejectsNonFiniteAndOverflow(t *testing.T) {
	plan := aggregatePlan("sum")
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := NewBuckets(plan).Add(plan.From, value); !errors.Is(err, ErrArithmetic) {
			t.Fatal("non-finite sample entered aggregation")
		}
	}
	buckets := NewBuckets(plan)
	if err := buckets.Add(plan.From, math.MaxFloat64); err != nil {
		t.Fatal(err)
	}
	if err := buckets.Add(plan.From.Add(time.Second), math.MaxFloat64); !errors.Is(err, ErrArithmetic) {
		t.Fatal("sum overflow was not rejected")
	}
	plan.Aggregation = "avg"
	buckets = NewBuckets(plan)
	for range 2 {
		if err := buckets.Add(plan.From, math.MaxFloat64); err != nil {
			t.Fatal(err)
		}
	}
	points, err := buckets.Points()
	if err != nil || points[0].Value == nil || math.IsInf(*points[0].Value, 0) {
		t.Fatal("finite average overflowed")
	}
}

func TestMetricQueryValidationBoundsRangeSeriesAndReferences(t *testing.T) {
	from := time.Unix(1_700_000_000, 0).UTC()
	valid := Request{From: from, To: from.Add(time.Hour), MaxPoints: 600, Series: []SeriesRequest{{RefID: "A", NodeID: "node-1", Metric: "cpu", Aggregation: "avg"}}}
	interval, count, err := ValidateRequest(valid)
	if err != nil || interval != 6*time.Second || count != 600 {
		t.Fatalf("valid plan: %v/%d/%v", interval, count, err)
	}
	for name, mutate := range map[string]func(*Request){
		"reversed": func(r *Request) { r.To = r.From }, "long range": func(r *Request) { r.To = r.From.Add(MaximumRange + time.Second) },
		"point budget": func(r *Request) { r.MaxPoints = MaximumPoints + 1 }, "missing series": func(r *Request) { r.Series = nil },
		"duplicate ref": func(r *Request) { r.Series = append(r.Series, r.Series[0]) }, "too many series": func(r *Request) { r.Series = make([]SeriesRequest, MaximumSeries+1) },
		"empty node": func(r *Request) { r.Series[0].NodeID = "" }, "unsafe metric": func(r *Request) { r.Series[0].Metric = "cpu; select" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			request.Series = append([]SeriesRequest(nil), valid.Series...)
			mutate(&request)
			if _, _, err := ValidateRequest(request); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid query accepted")
			}
		})
	}
}
