package metricquery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/metriccatalog"
	"github.com/beat/backend/internal/model"
)

type fakeCatalog struct {
	definitions map[string]model.MetricDefinition
	err         error
}

func (catalog fakeCatalog) Get(_ context.Context, key string) (*model.MetricDefinition, error) {
	value, ok := catalog.definitions[key]
	if !ok {
		return nil, catalog.err
	}
	return &value, catalog.err
}

type fakeNodes struct {
	visible bool
	err     error
}

func (nodes fakeNodes) GetNode(context.Context, string) (*model.Node, error) {
	return &model.Node{ID: "node"}, nodes.err
}

func (nodes fakeNodes) GetPublicNode(context.Context, string) (*model.Node, error) {
	if !nodes.visible {
		return nil, nodes.err
	}
	return &model.Node{ID: "node"}, nodes.err
}

type fakeReader struct {
	calls atomic.Int64
	read  func(context.Context, Scan) (Values, error)
}

func (reader *fakeReader) ReadMetricQuery(ctx context.Context, scan Scan) (Values, error) {
	reader.calls.Add(1)
	if reader.read != nil {
		return reader.read(ctx, scan)
	}
	points, err := NewBuckets(scan).Points()
	return Values{Points: points}, err
}

func validQueryFixture() (Request, fakeCatalog) {
	plan := aggregatePlan("avg")
	request := Request{From: plan.From, To: plan.To, MaxPoints: 3, Series: []SeriesRequest{{RefID: "A", NodeID: "node", Metric: "cpu"}}}
	return request, fakeCatalog{definitions: map[string]model.MetricDefinition{"cpu": metriccatalog.Builtins()[0]}}
}

func TestMetricQueryRejectsHiddenUnknownDisabledAndUnsupportedBeforeMTS(t *testing.T) {
	for _, problem := range []string{"unknown", "private", "disabled", "hidden node", "unsupported aggregation"} {
		t.Run(problem, func(t *testing.T) {
			request, catalog := validQueryFixture()
			nodes := fakeNodes{visible: true}
			definition := catalog.definitions["cpu"]
			switch problem {
			case "unknown":
				request.Series[0].Metric = "unknown"
			case "private":
				definition.Public = false
			case "disabled":
				definition.Enabled = false
			case "hidden node":
				nodes.visible = false
			case "unsupported aggregation":
				request.Series[0].Aggregation = "rate"
			}
			catalog.definitions["cpu"] = definition
			reader := &fakeReader{}
			if _, err := NewService(catalog, nodes, reader).Query(t.Context(), request, Access{PublicOnly: true, ClientKey: "client"}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("private/invalid query error=%v", err)
			}
			if reader.calls.Load() != 0 {
				t.Fatal("invalid query scanned MTS")
			}
		})
	}
}

func TestMetricQueryDistinguishesHealthyEmptyAndStorageFailure(t *testing.T) {
	request, catalog := validQueryFixture()
	reader := &fakeReader{}
	service := NewService(catalog, fakeNodes{visible: true}, reader)
	response, err := service.Query(t.Context(), request, Access{PublicOnly: true, ClientKey: "client"})
	if err != nil || len(response.Series) != 1 || response.Series[0].State != "empty" || response.Series[0].Aggregation != "avg" || len(response.Series[0].Points) != 3 {
		t.Fatalf("empty series contract=%v/%v", response, err)
	}
	if !response.From.Equal(request.From) || !response.To.Equal(request.To) || response.IntervalMS != 20_000 {
		t.Fatal("historical query range drifted")
	}
	reader.read = func(context.Context, Scan) (Values, error) { return Values{}, errors.New("disk failed") }
	if _, err := service.Query(t.Context(), request, Access{ClientKey: "client"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("storage failure returned a successful empty result")
	}
}

func TestMetricQueryAdminMayReadPrivateSeriesAndRateUnit(t *testing.T) {
	request, catalog := validQueryFixture()
	for _, definition := range metriccatalog.Builtins() {
		catalog.definitions[definition.Key] = definition
	}
	definition := catalog.definitions["net_recv_total"]
	definition.Public = false
	catalog.definitions[definition.Key] = definition
	request.Series[0].Metric, request.Series[0].Aggregation = definition.Key, "rate"
	reader := &fakeReader{read: func(_ context.Context, scan Scan) (Values, error) {
		buckets := NewBuckets(scan)
		if err := buckets.Add(scan.From.Add(-time.Second), 10); err != nil {
			return Values{}, err
		}
		if err := buckets.Add(scan.From, 20); err != nil {
			return Values{}, err
		}
		points, err := buckets.Points()
		return Values{Points: points}, err
	}}
	response, err := NewService(catalog, fakeNodes{}, reader).Query(t.Context(), request, Access{ClientKey: "admin"})
	if err != nil || response.Series[0].State != "ok" || response.Series[0].Unit != "bytes_per_second" {
		t.Fatalf("admin rate=%v/%v", response, err)
	}
}

func TestMetricQueryAdmissionBoundsClientsAndCancellationReleasesSlots(t *testing.T) {
	request, catalog := validQueryFixture()
	started := make(chan struct{}, MaximumConcurrent)
	reader := &fakeReader{read: func(ctx context.Context, _ Scan) (Values, error) {
		started <- struct{}{}
		<-ctx.Done()
		return Values{}, ctx.Err()
	}}
	service := NewService(catalog, fakeNodes{visible: true}, reader)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, MaximumPerClient)
	for range MaximumPerClient {
		go func() { _, err := service.Query(ctx, request, Access{ClientKey: "same"}); done <- err }()
	}
	for range MaximumPerClient {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("query did not acquire slot")
		}
	}
	if _, err := service.Query(t.Context(), request, Access{ClientKey: "same"}); !errors.Is(err, ErrCapacity) {
		t.Fatal("per-client query ceiling bypassed")
	}
	cancel()
	for range MaximumPerClient {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost its error")
		}
	}
	reader.read = nil
	if _, err := service.Query(t.Context(), request, Access{ClientKey: "same"}); err != nil {
		t.Fatal("canceled query leaked capacity")
	}
}
