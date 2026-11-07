package networkquality

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

type runnerTaskSource struct {
	tasks  []model.ServerNetworkTask
	err    error
	calls  atomic.Int32
	locked sync.Mutex
}

func (source *runnerTaskSource) ListServerTasks(context.Context) ([]model.ServerNetworkTask, error) {
	source.calls.Add(1)
	source.locked.Lock()
	defer source.locked.Unlock()
	return source.tasks, source.err
}

type recordingProber struct {
	started     chan model.NetworkAssignment
	release     <-chan struct{}
	result      model.NetworkProbeResult
	assignments []model.NetworkAssignment
	calls       atomic.Int32
	locked      sync.Mutex
}

func (prober *recordingProber) Probe(ctx context.Context, assignment model.NetworkAssignment) model.NetworkProbeResult {
	prober.calls.Add(1)
	prober.locked.Lock()
	prober.assignments = append(prober.assignments, assignment)
	prober.locked.Unlock()
	if prober.started != nil {
		prober.started <- assignment
	}
	if prober.release != nil {
		select {
		case <-prober.release:
		case <-ctx.Done():
		}
	}
	result := prober.result
	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now().UTC()
	}
	return result
}

func (prober *recordingProber) callsCount() int { return int(prober.calls.Load()) }

func (prober *recordingProber) recorded() []model.NetworkAssignment {
	prober.locked.Lock()
	defer prober.locked.Unlock()
	return append([]model.NetworkAssignment(nil), prober.assignments...)
}

type recordingWriter struct {
	samples []store.NetworkProbeSample
	err     error
	locked  sync.Mutex
}

func (writer *recordingWriter) WriteNetworkProbes(_ context.Context, samples []store.NetworkProbeSample) error {
	writer.locked.Lock()
	defer writer.locked.Unlock()
	writer.samples = append(writer.samples, samples...)
	return writer.err
}

func (writer *recordingWriter) recorded() []store.NetworkProbeSample {
	writer.locked.Lock()
	defer writer.locked.Unlock()
	return append([]store.NetworkProbeSample(nil), writer.samples...)
}

func TestNewRunnerDefaultsAndIncompleteOptions(t *testing.T) {
	runner := NewRunner(RunnerOptions{})
	if runner.options.RefreshInterval != defaultRefreshInterval || runner.options.ScheduleTick != defaultScheduleTick ||
		runner.options.Workers != defaultWorkers {
		t.Fatalf("runner defaults = %#v", runner.options)
	}
	runner.options.OnError(errors.New("ignored"))
	runner.options.OnCycle(errors.New("ignored"))
	if err := runner.Run(t.Context()); err == nil {
		t.Fatal("expected incomplete runner error")
	}
}

func TestRunnerProbeWritesServerSample(t *testing.T) {
	now := time.Now().UTC()
	loss, jitter, minimum, maximum := 25.0, 1.5, 2.0, 8.0
	prober := &recordingProber{result: model.NetworkProbeResult{
		FinishedAt: now, LatencyMS: 5, Success: true, ErrorCode: "none",
		SentPackets: 4, ReceivedPackets: 3, LossPercent: &loss, JitterMS: &jitter,
		MinLatencyMS: &minimum, MaxLatencyMS: &maximum,
	}}
	writer := &recordingWriter{}
	runner := NewRunner(RunnerOptions{Prober: prober, Writer: writer})
	job := serverProbeJob{
		key: "task\x00node",
		task: model.ServerNetworkTask{ID: "task", Name: "Edge", Type: model.NetworkProbeICMP,
			Target: "{{host}}", IPFamily: model.IPFamilyAuto, IntervalSeconds: 60,
			TimeoutMilliseconds: 1000, PacketCount: 4},
		target: model.NetworkProbeTarget{NodeID: "node", Host: "192.0.2.1", Port: 22},
	}
	if finished := runner.probe(t.Context(), job); !finished.Equal(now) {
		t.Fatalf("finished = %v, want %v", finished, now)
	}
	assignments := prober.recorded()
	if len(assignments) != 1 || assignments[0].Target != "192.0.2.1" ||
		assignments[0].Direction != model.NetworkDirectionServerToAgent || assignments[0].PacketCount != 4 {
		t.Fatalf("assignments = %#v", assignments)
	}
	samples := writer.recorded()
	if len(samples) != 1 || samples[0].Direction != model.NetworkDirectionServerToAgent ||
		samples[0].NodeID != "node" || samples[0].LossPercent == nil || *samples[0].LossPercent != loss {
		t.Fatalf("samples = %#v", samples)
	}
}

func TestRunnerProbeReportsTargetAndWriteErrors(t *testing.T) {
	errorsSeen := make(chan error, 2)
	prober := &recordingProber{}
	writer := &recordingWriter{err: errors.New("storage down")}
	runner := NewRunner(RunnerOptions{
		Prober: prober, Writer: writer,
		OnError: func(err error) { errorsSeen <- err },
	})
	invalid := serverProbeJob{
		task:   model.ServerNetworkTask{ID: "task", Type: model.NetworkProbeICMP, Target: "{{unknown}}"},
		target: model.NetworkProbeTarget{NodeID: "node", Host: "192.0.2.1", Port: 22},
	}
	runner.probe(t.Context(), invalid)
	if prober.callsCount() != 0 || len(writer.recorded()) != 0 {
		t.Fatal("invalid target should not probe or write")
	}
	select {
	case err := <-errorsSeen:
		if !strings.Contains(err.Error(), "render") {
			t.Fatalf("unexpected render error: %v", err)
		}
	default:
		t.Fatal("expected render error")
	}

	valid := invalid
	valid.task.Target = "{{host}}"
	runner.probe(t.Context(), valid)
	if prober.callsCount() != 1 {
		t.Fatalf("probe calls = %d", prober.callsCount())
	}
	select {
	case err := <-errorsSeen:
		if !strings.Contains(err.Error(), "write") {
			t.Fatalf("unexpected write error: %v", err)
		}
	default:
		t.Fatal("expected write error")
	}
}

func TestMergeServerTasksAndEnqueueDue(t *testing.T) {
	now := time.Now()
	current := map[string]*serverProbeState{
		"task\x00node":  {running: true, next: now.Add(time.Minute)},
		"stale\x00node": {next: now},
	}
	tasks := []model.ServerNetworkTask{{
		ID: "task", Type: model.NetworkProbeICMP, Target: "{{host}}", IntervalSeconds: 60,
		Targets: []model.NetworkProbeTarget{{NodeID: "node", Host: "192.0.2.1", Port: 22}},
	}}
	merged := mergeServerTasks(current, tasks, now)
	state := merged["task\x00node"]
	if state == nil || !state.running || !state.next.Equal(now.Add(time.Minute)) || merged["stale\x00node"] != nil {
		t.Fatalf("merged states = %#v", merged)
	}

	due := &serverProbeState{task: tasks[0], target: tasks[0].Targets[0], next: now}
	running := &serverProbeState{task: tasks[0], target: tasks[0].Targets[0], next: now, running: true}
	future := &serverProbeState{task: tasks[0], target: tasks[0].Targets[0], next: now.Add(time.Minute)}
	work := make(chan serverProbeJob, 2)
	enqueueDueServerProbes(map[string]*serverProbeState{"due": due, "running": running, "future": future}, now, work)
	if len(work) != 1 || !due.running || !running.running || future.running {
		t.Fatalf("enqueued = %d, due = %#v, running = %#v, future = %#v", len(work), due, running, future)
	}
}

func TestRunnerStartsImmediatelyAndStops(t *testing.T) {
	started := make(chan model.NetworkAssignment, 1)
	prober := &recordingProber{started: started}
	writer := &recordingWriter{}
	source := &runnerTaskSource{tasks: []model.ServerNetworkTask{{
		ID: "task", Type: model.NetworkProbeICMP, Target: "{{host}}", IPFamily: model.IPFamilyAuto,
		IntervalSeconds: 60, TimeoutMilliseconds: 1000, PacketCount: 1,
		Targets: []model.NetworkProbeTarget{{NodeID: "node", Host: "192.0.2.1", Port: 22}},
	}}}
	ctx, cancel := context.WithCancel(t.Context())
	runner := NewRunner(RunnerOptions{
		Tasks: source, Prober: prober, Writer: writer,
		RefreshInterval: time.Hour, ScheduleTick: 5 * time.Millisecond, Workers: 1,
	})
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case assignment := <-started:
		if assignment.Direction != model.NetworkDirectionServerToAgent || assignment.Target != "192.0.2.1" {
			t.Fatalf("initial assignment = %#v", assignment)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not probe immediately")
	}
	time.Sleep(20 * time.Millisecond)
	if prober.callsCount() != 1 {
		t.Fatalf("overlapping or duplicate probes = %d", prober.callsCount())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runner stopped with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}
