package metricquery

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

type fakeLatestReader struct {
	fakeReader
	latest func(context.Context, LatestScan) (LatestValue, error)
}

func (reader *fakeLatestReader) ReadMetricLatest(ctx context.Context, scan LatestScan) (LatestValue, error) {
	if reader.latest == nil {
		return LatestValue{}, nil
	}
	return reader.latest(ctx, scan)
}

func latestFixture() (LatestRequest, fakeCatalog, *fakeLatestReader) {
	reader := &fakeLatestReader{}
	reader.latest = func(context.Context, LatestScan) (LatestValue, error) {
		return LatestValue{Found: true, SampleAt: time.Unix(1_700_000_000, 0).UTC(), Value: 41.5}, nil
	}
	return LatestRequest{Series: []LatestSeriesRequest{{RefID: "A", NodeID: "node", Metric: "cpu"}}},
		fakeCatalog{definitions: map[string]model.MetricDefinition{"cpu": metriccatalog.Builtins()[0]}}, reader
}

func TestLatestRequestValidation(t *testing.T) {
	valid := func() LatestRequest {
		request, _, _ := latestFixture()
		return request
	}
	for _, test := range []struct {
		name   string
		mutate func(*LatestRequest)
	}{
		{"empty", func(request *LatestRequest) { request.Series = nil }},
		{"duplicate reference", func(request *LatestRequest) {
			request.Series = append(request.Series, request.Series[0])
		}},
		{"blank reference", func(request *LatestRequest) { request.Series[0].RefID = "" }},
		{"oversized reference", func(request *LatestRequest) { request.Series[0].RefID = string(make([]byte, MaximumReferenceBytes+1)) }},
		{"invalid node", func(request *LatestRequest) { request.Series[0].NodeID = "../etc" }},
		{"invalid metric", func(request *LatestRequest) { request.Series[0].Metric = "Not A Key" }},
		{"too many series", func(request *LatestRequest) {
			for index := 0; index <= MaximumLatestSeries; index++ {
				request.Series = append(request.Series, LatestSeriesRequest{RefID: fmt.Sprintf("s%d", index), NodeID: "node", Metric: "cpu"})
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := valid()
			test.mutate(&request)
			if err := ValidateLatestRequest(request); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid latest request accepted", err)
			}
		})
	}
	request := valid()
	for index := 0; index < MaximumLatestSeries-1; index++ {
		request.Series = append(request.Series, LatestSeriesRequest{RefID: fmt.Sprintf("s%d", index), NodeID: "node", Metric: "cpu"})
	}
	if err := ValidateLatestRequest(request); err != nil {
		t.Fatal("bounded latest request rejected", err)
	}
}

func TestLatestReturnsLatestSamplesAndEmptyState(t *testing.T) {
	request, catalog, reader := latestFixture()
	request.Series = append(request.Series, LatestSeriesRequest{RefID: "B", NodeID: "node", Metric: "memory"})
	catalog.definitions["memory"] = metriccatalog.Builtins()[1]
	reader.latest = func(_ context.Context, scan LatestScan) (LatestValue, error) {
		if scan.Metric == "memory" {
			return LatestValue{}, nil
		}
		return LatestValue{Found: true, SampleAt: time.Unix(1_700_000_000, 0).UTC(), Value: 2.5}, nil
	}
	service := NewService(catalog, fakeNodes{visible: true}, reader)
	result, err := service.Latest(t.Context(), request, Access{ClientKey: "client"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Series) != 2 || result.Series[0].State != "ok" || result.Series[0].Value == nil || *result.Series[0].Value != 2.5 || result.Series[0].SampleAt == nil {
		t.Fatalf("unexpected latest series: %+v", result.Series)
	}
	if result.Series[1].State != "empty" || result.Series[1].Value != nil || result.Series[1].SampleAt != nil {
		t.Fatalf("unexpected empty series: %+v", result.Series[1])
	}
	if result.Series[0].Label == "" || result.Series[0].Metric != "cpu" {
		t.Fatalf("latest series lost catalogue metadata: %+v", result.Series[0])
	}
}

func TestLatestRejectsUnavailableInputsBeforeStorage(t *testing.T) {
	for _, problem := range []string{"unknown metric", "disabled", "private", "hidden node", "reader error", "non-finite", "zero time"} {
		t.Run(problem, func(t *testing.T) {
			request, catalog, reader := latestFixture()
			nodes := fakeNodes{visible: true}
			definition := catalog.definitions["cpu"]
			switch problem {
			case "unknown metric":
				request.Series[0].Metric = "unknown"
			case "disabled":
				definition.Enabled = false
			case "private":
				definition.Public = false
			case "hidden node":
				nodes.visible = false
			case "reader error":
				reader.latest = func(context.Context, LatestScan) (LatestValue, error) {
					return LatestValue{}, errors.New("disk failed")
				}
			case "non-finite":
				reader.latest = func(context.Context, LatestScan) (LatestValue, error) {
					return LatestValue{Found: true, SampleAt: time.Unix(1_700_000_000, 0), Value: math.Inf(1)}, nil
				}
			case "zero time":
				reader.latest = func(context.Context, LatestScan) (LatestValue, error) { return LatestValue{Found: true, Value: 1}, nil }
			}
			catalog.definitions["cpu"] = definition
			want := ErrUnavailable
			if problem == "unknown metric" || problem == "disabled" || problem == "private" || problem == "hidden node" {
				want = ErrInvalid
			}
			service := NewService(catalog, nodes, reader)
			public := problem == "private" || problem == "hidden node"
			if _, err := service.Latest(t.Context(), request, Access{PublicOnly: public}); !errors.Is(err, want) {
				t.Fatal("unexpected latest error", problem, err)
			}
		})
	}
}

func TestLatestRequiresLatestCapableReaderAndPassesCapacityTruthfully(t *testing.T) {
	request, catalog, _ := latestFixture()
	service := NewService(catalog, fakeNodes{visible: true}, &fakeReader{})
	if _, err := service.Latest(t.Context(), request, Access{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("reader without latest capability served latest query", err)
	}
	reader := &fakeLatestReader{}
	reader.latest = func(context.Context, LatestScan) (LatestValue, error) { return LatestValue{}, ErrCapacity }
	service = NewService(catalog, fakeNodes{visible: true}, reader)
	if _, err := service.Latest(t.Context(), request, Access{}); !errors.Is(err, ErrCapacity) {
		t.Fatal("capacity error was masked", err)
	}
	var missing *Service
	if _, err := missing.Latest(t.Context(), request, Access{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil service served latest query", err)
	}
	if _, err := service.Latest(t.Context(), LatestRequest{}, Access{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty latest request accepted", err)
	}
}
