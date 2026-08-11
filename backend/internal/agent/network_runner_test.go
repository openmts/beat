package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
)

type fakeNetworkClient struct {
	set       NetworkAssignmentSet
	fetchErr  error
	reportErr error
	reported  chan []model.NetworkProbeResult
}

func (client *fakeNetworkClient) FetchAssignments(context.Context) (NetworkAssignmentSet, error) {
	return client.set, client.fetchErr
}

func (client *fakeNetworkClient) ReportResults(_ context.Context, results []model.NetworkProbeResult) error {
	if client.reportErr != nil {
		return client.reportErr
	}
	copyOfResults := append([]model.NetworkProbeResult(nil), results...)
	select {
	case client.reported <- copyOfResults:
	default:
	}
	return nil
}

type trackingProber struct {
	calls   atomic.Int32
	active  atomic.Int32
	maximum atomic.Int32
}

func (prober *trackingProber) Probe(ctx context.Context, task model.NetworkAssignment) model.NetworkProbeResult {
	prober.calls.Add(1)
	active := prober.active.Add(1)
	for {
		maximum := prober.maximum.Load()
		if active <= maximum || prober.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Millisecond):
	}
	prober.active.Add(-1)
	return model.NetworkProbeResult{TaskID: task.ID, FinishedAt: time.Now(), Success: true, ErrorCode: "none"}
}

func TestNetworkRunnerSchedulesReportsAndStops(t *testing.T) {
	client := &fakeNetworkClient{
		set: NetworkAssignmentSet{
			ExpiresAt: time.Now().Add(time.Minute),
			Tasks:     []model.NetworkAssignment{{ID: "task", IntervalSeconds: 10, TimeoutMilliseconds: 1000}},
		},
		reported: make(chan []model.NetworkProbeResult, 1),
	}
	prober := &trackingProber{}
	runner := NewNetworkRunner(NetworkRunnerOptions{
		Client: client, Prober: prober, RefreshInterval: time.Hour,
		ScheduleTick: time.Millisecond, FlushInterval: time.Millisecond, Workers: 4,
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case results := <-client.reported:
		if len(results) != 1 || results[0].TaskID != "task" {
			t.Fatalf("reported = %#v", results)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if prober.maximum.Load() != 1 {
		t.Fatalf("maximum overlap = %d", prober.maximum.Load())
	}
}

func TestNetworkRunnerHonorsExpiryAndReportsErrors(t *testing.T) {
	errorsSeen := make(chan error, 4)
	client := &fakeNetworkClient{
		set: NetworkAssignmentSet{
			ExpiresAt: time.Now().Add(-time.Second),
			Tasks:     []model.NetworkAssignment{{ID: "expired", IntervalSeconds: 10}},
		},
		fetchErr: errors.New("offline"), reported: make(chan []model.NetworkProbeResult, 1),
	}
	prober := &trackingProber{}
	runner := NewNetworkRunner(NetworkRunnerOptions{
		Client: client, Prober: prober, RefreshInterval: 2 * time.Millisecond,
		ScheduleTick: time.Millisecond, FlushInterval: time.Millisecond, Workers: 1,
		OnError: func(err error) {
			select {
			case errorsSeen <- err:
			default:
			}
		},
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prober.calls.Load() != 0 {
		t.Fatalf("expired probe calls = %d", prober.calls.Load())
	}
	select {
	case <-errorsSeen:
	default:
		t.Fatal("expected refresh error")
	}
}

func TestMergeAndEnqueueNetworkAssignments(t *testing.T) {
	now := time.Now()
	states := mergeNetworkAssignments(nil, NetworkAssignmentSet{
		ExpiresAt: now.Add(time.Minute), Tasks: []model.NetworkAssignment{{ID: "task", IntervalSeconds: 10}},
	})
	work := make(chan model.NetworkAssignment, 1)
	enqueueDueNetworkTasks(states, now.Add(time.Second), work)
	if len(work) != 1 || !states["task"].running {
		t.Fatalf("state = %#v, work = %d", states["task"], len(work))
	}
}

func TestNetworkRunnerDefaultsAndFlushBranches(t *testing.T) {
	errorsSeen := []error{}
	client := &fakeNetworkClient{reported: make(chan []model.NetworkProbeResult, 1)}
	runner := NewNetworkRunner(NetworkRunnerOptions{
		Client: client,
		Prober: &trackingProber{},
		OnError: func(err error) {
			errorsSeen = append(errorsSeen, err)
		},
	})
	if runner.options.RefreshInterval != defaultAssignmentRefresh ||
		runner.options.ScheduleTick != defaultScheduleTick ||
		runner.options.FlushInterval != defaultResultFlush ||
		runner.options.Workers != defaultProbeWorkers {
		t.Fatalf("runner defaults = %#v", runner.options)
	}
	defaultRunner := NewNetworkRunner(NetworkRunnerOptions{})
	defaultRunner.options.OnError(errors.New("ignored"))
	if got := runner.flushResults(t.Context(), nil); len(got) != 0 {
		t.Fatalf("empty flush = %#v", got)
	}
	batch := []model.NetworkProbeResult{{TaskID: "one"}, {TaskID: "two"}}
	if got := runner.flushResults(t.Context(), batch); len(got) != 0 {
		t.Fatalf("successful flush = %#v", got)
	}
	client.reportErr = errors.New("offline")
	if got := runner.flushResults(t.Context(), batch); len(got) != len(batch) {
		t.Fatalf("failed flush = %#v", got)
	}
	if len(errorsSeen) != 1 {
		t.Fatalf("flush errors = %#v", errorsSeen)
	}

	now := time.Now()
	states := mergeNetworkAssignments(nil, NetworkAssignmentSet{
		ExpiresAt: now.Add(time.Minute), Tasks: []model.NetworkAssignment{{ID: "blocked", IntervalSeconds: 10}},
	})
	work := make(chan model.NetworkAssignment)
	enqueueDueNetworkTasks(states, now.Add(time.Second), work)
	if states["blocked"].running {
		t.Fatal("blocked task should remain pending")
	}

	client.fetchErr = nil
	client.set = NetworkAssignmentSet{Tasks: []model.NetworkAssignment{{ID: "new"}}}
	updates := make(chan NetworkAssignmentSet, 1)
	updates <- NetworkAssignmentSet{Tasks: []model.NetworkAssignment{{ID: "old"}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var workers sync.WaitGroup
	workers.Add(1)
	runner.refreshAssignments(ctx, &workers, updates)
	workers.Wait()
	if got := <-updates; len(got.Tasks) != 1 || got.Tasks[0].ID != "new" {
		t.Fatalf("latest assignments = %#v", got)
	}
}

func TestNetworkRunnerResultBatchKeepsCapacityAfterFlushes(t *testing.T) {
	client := &fakeNetworkClient{reported: make(chan []model.NetworkProbeResult, 16)}
	runner := NewNetworkRunner(NetworkRunnerOptions{
		Client: client, Prober: &trackingProber{}, FlushInterval: time.Hour,
	})
	results := make(chan model.NetworkProbeResult, defaultResultQueueSize+1)
	for index := range defaultResultQueueSize + 1 {
		results <- model.NetworkProbeResult{TaskID: fmt.Sprintf("task-%d", index)}
	}

	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	workers.Add(1)
	panicValue := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { panicValue <- recover() }()
		runner.reportResults(ctx, &workers, results)
	}()

	deadline := time.After(time.Second)
	for len(results) > 0 {
		select {
		case <-done:
			value := <-panicValue
			t.Fatalf("result reporter panicked: %v", value)
		case <-deadline:
			t.Fatal("timed out draining result queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	<-done
	workers.Wait()
	if value := <-panicValue; value != nil {
		t.Fatalf("result reporter panicked: %v", value)
	}
}
