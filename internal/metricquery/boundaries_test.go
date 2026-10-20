package metricquery

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMetricQueryAdmissionHasGlobalLimitAndIdempotentRelease(t *testing.T) {
	budget := newAdmission()
	release := make([]func(), 0, MaximumConcurrent)
	for index := range MaximumConcurrent {
		unlock, err := budget.acquire(t.Context(), string(rune('a'+index)))
		if err != nil {
			t.Fatal(err)
		}
		release = append(release, unlock)
	}
	if _, err := budget.acquire(t.Context(), "another"); !errors.Is(err, ErrCapacity) {
		t.Fatal("global query ceiling bypassed")
	}
	for _, unlock := range release {
		unlock()
		unlock()
	}
	if budget.active != 0 || len(budget.clients) != 0 {
		t.Fatal("admission leaked client entries")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := budget.acquire(ctx, "client"); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request acquired capacity")
	}
	if _, err := budget.acquire(t.Context(), strings.Repeat("x", MaximumNodeIDBytes+1)); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded client key accepted")
	}
	unlock, err := budget.acquire(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestMetricQueryRejectsUnrepresentableAndMalformedTimestamps(t *testing.T) {
	request, _ := validQueryFixture()
	for _, change := range []func(*Request){
		func(r *Request) { r.From = time.Unix(-1, 0) }, func(r *Request) { r.To = time.Unix(0, math.MaxInt64).Add(time.Second) },
		func(r *Request) { r.From = r.From.Add(time.Nanosecond) }, func(r *Request) { r.Series[0].RefID = "bad ref" },
		func(r *Request) { r.Series[0].Aggregation = "unsupported" }, func(r *Request) { r.Series[0].RefID = strings.Repeat("a", MaximumReferenceBytes+1) },
	} {
		copy := request
		copy.Series = append([]SeriesRequest(nil), request.Series...)
		change(&copy)
		if _, _, err := ValidateRequest(copy); !errors.Is(err, ErrInvalid) {
			t.Fatal("unrepresentable query accepted")
		}
	}
}

func TestMetricAggregationRejectsInvalidPlansAndCounterObservations(t *testing.T) {
	for _, change := range []func(*Scan){func(p *Scan) { p.Buckets = 0 }, func(p *Scan) { p.Interval = 0 }, func(p *Scan) { p.Aggregation = "unknown" }} {
		plan := aggregatePlan("avg")
		change(&plan)
		buckets := NewBuckets(plan)
		if _, err := buckets.Points(); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid plan produced points")
		}
		if err := buckets.Add(plan.From, 1); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid plan consumed a sample")
		}
	}
	plan := aggregatePlan("rate")
	for _, value := range []float64{-1, math.NaN()} {
		if err := NewBuckets(plan).Add(plan.From, value); !errors.Is(err, ErrArithmetic) {
			t.Fatal("invalid counter accepted")
		}
	}
	buckets := NewBuckets(plan)
	if err := buckets.Add(plan.From, 1); err != nil {
		t.Fatal(err)
	}
	if err := buckets.Add(plan.From, 2); !errors.Is(err, ErrArithmetic) {
		t.Fatal("zero-duration rate was invented")
	}
	plan = aggregatePlan("avg")
	buckets = NewBuckets(plan)
	if err := buckets.Add(plan.From.Add(-time.Second), 9); err != nil {
		t.Fatal(err)
	}
	points, err := buckets.Points()
	if err != nil || points[0].Value != nil {
		t.Fatal("pre-range gauge contaminated first bucket")
	}
}

func TestMetricQueryRejectsDependencyAndInvalidReaderResults(t *testing.T) {
	request, catalog := validQueryFixture()
	reader := &fakeReader{}
	for _, service := range []*Service{nil, NewService(nil, fakeNodes{}, reader), NewService(fakeCatalog{err: errors.New("catalog failed")}, fakeNodes{visible: true}, reader), NewService(catalog, fakeNodes{err: errors.New("nodes failed")}, reader)} {
		if _, err := service.Query(t.Context(), request, Access{}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("dependency failure became empty success")
		}
	}
	for _, failure := range []error{ErrCapacity, ErrArithmetic} {
		reader.read = func(context.Context, Scan) (Values, error) { return Values{}, failure }
		if _, err := NewService(catalog, fakeNodes{}, reader).Query(t.Context(), request, Access{}); !errors.Is(err, failure) {
			t.Fatal("typed query failure changed")
		}
	}
	for _, fault := range []string{"size", "time", "value"} {
		reader.read = func(_ context.Context, scan Scan) (Values, error) {
			points, _ := NewBuckets(scan).Points()
			switch fault {
			case "size":
				points = nil
			case "time":
				points[0].Timestamp++
			case "value":
				value := math.Inf(1)
				points[0].Value = &value
			}
			return Values{Points: points}, nil
		}
		if _, err := NewService(catalog, fakeNodes{}, reader).Query(t.Context(), request, Access{}); !errors.Is(err, ErrUnavailable) {
			t.Fatal("corrupt reader result was published")
		}
	}
}

func TestMetricLegacyReadsShareQueryAdmission(t *testing.T) {
	request, catalog := validQueryFixture()
	service := NewService(catalog, fakeNodes{}, &fakeReader{})
	first, err := service.budget.acquire(t.Context(), "client")
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	second, err := service.budget.acquire(t.Context(), "client")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReadWithBudget(t.Context(), "client", func(context.Context) error { t.Fatal("excess legacy query ran"); return nil }); !errors.Is(err, ErrCapacity) {
		t.Fatal("legacy route bypassed admission")
	}
	second()
	if err := service.ReadWithBudget(t.Context(), "client", func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request.MaxPoints = 1
	if _, err := service.Query(t.Context(), request, Access{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid query reached storage")
	}
}
