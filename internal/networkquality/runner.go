package networkquality

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/store"
)

const (
	defaultRefreshInterval = 30 * time.Second
	defaultScheduleTick    = 250 * time.Millisecond
	defaultWorkers         = 4
	defaultWorkQueue       = 128
)

type TaskSource interface {
	ListServerTasks(context.Context) ([]model.ServerNetworkTask, error)
}

type ProbeExecutor interface {
	Probe(context.Context, model.NetworkAssignment) model.NetworkProbeResult
}

type ResultWriter interface {
	WriteNetworkProbes(context.Context, []store.NetworkProbeSample) error
}

type RunnerOptions struct {
	Tasks           TaskSource
	Prober          ProbeExecutor
	Writer          ResultWriter
	RefreshInterval time.Duration
	ScheduleTick    time.Duration
	Workers         int
	OnError         func(error)
	OnCycle         func(error)
}

type Runner struct {
	options RunnerOptions
}

type serverProbeState struct {
	task    model.ServerNetworkTask
	target  model.NetworkProbeTarget
	next    time.Time
	running bool
}

type serverProbeJob struct {
	key    string
	task   model.ServerNetworkTask
	target model.NetworkProbeTarget
}

type serverProbeCompletion struct {
	key      string
	finished time.Time
}

func NewRunner(options RunnerOptions) *Runner {
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = defaultRefreshInterval
	}
	if options.ScheduleTick <= 0 {
		options.ScheduleTick = defaultScheduleTick
	}
	if options.Workers <= 0 {
		options.Workers = defaultWorkers
	}
	if options.OnError == nil {
		options.OnError = func(error) {}
	}
	if options.OnCycle == nil {
		options.OnCycle = func(error) {}
	}
	return &Runner{options: options}
}

func (runner *Runner) Run(ctx context.Context) error {
	if runner.options.Tasks == nil || runner.options.Prober == nil || runner.options.Writer == nil {
		return errors.New("server network quality runner is incomplete")
	}
	updates := make(chan []model.ServerNetworkTask, 1)
	work := make(chan serverProbeJob, defaultWorkQueue)
	completions := make(chan serverProbeCompletion, defaultWorkQueue)
	var workers sync.WaitGroup
	workers.Add(2 + runner.options.Workers)
	go runner.refresh(ctx, &workers, updates)
	go runner.schedule(ctx, &workers, updates, work, completions)
	for range runner.options.Workers {
		go runner.probeWorker(ctx, &workers, work, completions)
	}
	<-ctx.Done()
	workers.Wait()
	return nil
}

func (runner *Runner) refresh(
	ctx context.Context,
	workers *sync.WaitGroup,
	updates chan<- []model.ServerNetworkTask,
) {
	defer workers.Done()
	ticker := time.NewTicker(runner.options.RefreshInterval)
	defer ticker.Stop()
	for {
		tasks, err := runner.options.Tasks.ListServerTasks(ctx)
		runner.options.OnCycle(err)
		if err != nil && ctx.Err() == nil {
			runner.options.OnError(fmt.Errorf("refresh server network tasks: %w", err))
		} else if err == nil {
			select {
			case updates <- tasks:
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runner *Runner) schedule(
	ctx context.Context,
	workers *sync.WaitGroup,
	updates <-chan []model.ServerNetworkTask,
	work chan<- serverProbeJob,
	completions <-chan serverProbeCompletion,
) {
	defer workers.Done()
	states := map[string]*serverProbeState{}
	ticker := time.NewTicker(runner.options.ScheduleTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case tasks := <-updates:
			states = mergeServerTasks(states, tasks, model.NowUTC())
		case completion := <-completions:
			state := states[completion.key]
			if state == nil {
				continue
			}
			state.running = false
			state.next = completion.finished.Add(time.Duration(state.task.IntervalSeconds) * time.Second)
		case now := <-ticker.C:
			enqueueDueServerProbes(states, now, work)
		}
	}
}

func (runner *Runner) probeWorker(
	ctx context.Context,
	workers *sync.WaitGroup,
	work <-chan serverProbeJob,
	completions chan<- serverProbeCompletion,
) {
	defer workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-work:
			finished := runner.probe(ctx, job)
			select {
			case completions <- serverProbeCompletion{key: job.key, finished: finished}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (runner *Runner) probe(ctx context.Context, job serverProbeJob) time.Time {
	target, err := model.RenderNetworkTarget(job.task.Type, job.task.Target, job.target.Host, job.target.Port)
	if err != nil {
		runner.options.OnError(fmt.Errorf("render server network target: %w", err))
		runner.options.OnCycle(err)
		return model.NowUTC()
	}
	assignment := model.NetworkAssignment{
		ID: job.task.ID, Name: job.task.Name, Direction: model.NetworkDirectionServerToAgent,
		Type: job.task.Type, Target: target, IPFamily: job.task.IPFamily,
		IntervalSeconds: job.task.IntervalSeconds, TimeoutMilliseconds: job.task.TimeoutMilliseconds,
		PacketCount: job.task.PacketCount,
	}
	result := runner.options.Prober.Probe(ctx, assignment)
	if ctx.Err() != nil {
		return result.FinishedAt
	}
	sample := store.NetworkProbeSample{
		TaskID: job.task.ID, NodeID: job.target.NodeID, TaskType: job.task.Type,
		Direction: model.NetworkDirectionServerToAgent, FinishedAt: result.FinishedAt.UTC(),
		LatencyMS: result.LatencyMS, Success: result.Success, StatusCode: result.StatusCode,
		ErrorCode: result.ErrorCode, SentPackets: result.SentPackets,
		ReceivedPackets: result.ReceivedPackets, AttemptCount: result.AttemptCount,
		SuccessCount: result.SuccessCount, SuccessPercent: result.SuccessPercent,
		LossPercent: result.LossPercent, JitterMS: result.JitterMS,
		MinLatencyMS: result.MinLatencyMS, MaxLatencyMS: result.MaxLatencyMS,
		P95LatencyMS: result.P95LatencyMS, StdDevLatencyMS: result.StdDevLatencyMS,
	}
	if writeErr := runner.options.Writer.WriteNetworkProbes(ctx, []store.NetworkProbeSample{sample}); writeErr != nil {
		runner.options.OnError(fmt.Errorf("write server network probe: %w", writeErr))
		runner.options.OnCycle(writeErr)
	} else {
		runner.options.OnCycle(nil)
	}
	return result.FinishedAt
}

func mergeServerTasks(
	current map[string]*serverProbeState,
	tasks []model.ServerNetworkTask,
	now time.Time,
) map[string]*serverProbeState {
	next := map[string]*serverProbeState{}
	for _, task := range tasks {
		for _, target := range task.Targets {
			key := task.ID + "\x00" + target.NodeID
			state := current[key]
			if state == nil {
				state = &serverProbeState{next: now}
			}
			state.task = task
			state.target = target
			next[key] = state
		}
	}
	return next
}

func enqueueDueServerProbes(
	states map[string]*serverProbeState,
	now time.Time,
	work chan<- serverProbeJob,
) {
	for key, state := range states {
		if state.running || now.Before(state.next) {
			continue
		}
		select {
		case work <- serverProbeJob{key: key, task: state.task, target: state.target}:
			state.running = true
		default:
			return
		}
	}
}
