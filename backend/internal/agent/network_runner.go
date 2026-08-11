package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/beat/backend/internal/model"
)

const (
	defaultAssignmentRefresh = 30 * time.Second
	defaultScheduleTick      = 250 * time.Millisecond
	defaultResultFlush       = 2 * time.Second
	defaultProbeWorkers      = 4
	defaultWorkQueueSize     = 128
	defaultResultQueueSize   = 256
	defaultResultBatchSize   = 32
)

type ProbeExecutor interface {
	Probe(context.Context, model.NetworkAssignment) model.NetworkProbeResult
}

type NetworkRunnerOptions struct {
	Client          NetworkAssignmentClient
	Prober          ProbeExecutor
	RefreshInterval time.Duration
	ScheduleTick    time.Duration
	FlushInterval   time.Duration
	Workers         int
	OnError         func(error)
}

type NetworkRunner struct {
	options NetworkRunnerOptions
}

type scheduledNetworkTask struct {
	task    model.NetworkAssignment
	expires time.Time
	next    time.Time
	running bool
}

type networkProbeCompletion struct {
	taskID string
	result model.NetworkProbeResult
	report bool
}

func NewNetworkRunner(options NetworkRunnerOptions) *NetworkRunner {
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = defaultAssignmentRefresh
	}
	if options.ScheduleTick <= 0 {
		options.ScheduleTick = defaultScheduleTick
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = defaultResultFlush
	}
	if options.Workers <= 0 {
		options.Workers = defaultProbeWorkers
	}
	if options.OnError == nil {
		options.OnError = func(error) {}
	}
	return &NetworkRunner{options: options}
}

func (runner *NetworkRunner) Run(ctx context.Context) error {
	updates := make(chan NetworkAssignmentSet, 1)
	work := make(chan model.NetworkAssignment, defaultWorkQueueSize)
	completions := make(chan networkProbeCompletion, defaultWorkQueueSize)
	results := make(chan model.NetworkProbeResult, defaultResultQueueSize)
	var workers sync.WaitGroup
	workers.Add(3 + runner.options.Workers)
	go runner.refreshAssignments(ctx, &workers, updates)
	go runner.schedule(ctx, &workers, updates, work, completions, results)
	for range runner.options.Workers {
		go runner.probeWorker(ctx, &workers, work, completions)
	}
	go runner.reportResults(ctx, &workers, results)
	<-ctx.Done()
	workers.Wait()
	return nil
}

func (runner *NetworkRunner) refreshAssignments(
	ctx context.Context,
	workers *sync.WaitGroup,
	updates chan NetworkAssignmentSet,
) {
	defer workers.Done()
	ticker := time.NewTicker(runner.options.RefreshInterval)
	defer ticker.Stop()
	for {
		set, err := runner.options.Client.FetchAssignments(ctx)
		if err != nil && ctx.Err() == nil {
			runner.options.OnError(fmt.Errorf("refresh network assignments: %w", err))
		} else if err == nil {
			select {
			case updates <- set:
			default:
				<-updates
				updates <- set
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runner *NetworkRunner) schedule(
	ctx context.Context,
	workers *sync.WaitGroup,
	updates <-chan NetworkAssignmentSet,
	work chan<- model.NetworkAssignment,
	completions <-chan networkProbeCompletion,
	results chan<- model.NetworkProbeResult,
) {
	defer workers.Done()
	tasks := map[string]*scheduledNetworkTask{}
	ticker := time.NewTicker(runner.options.ScheduleTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case set := <-updates:
			tasks = mergeNetworkAssignments(tasks, set)
		case completion := <-completions:
			if task := tasks[completion.taskID]; task != nil {
				task.running = false
			}
			if completion.report {
				select {
				case results <- completion.result:
				case <-ctx.Done():
					return
				}
			}
		case now := <-ticker.C:
			enqueueDueNetworkTasks(tasks, now, work)
		}
	}
}

func (runner *NetworkRunner) probeWorker(
	ctx context.Context,
	workers *sync.WaitGroup,
	work <-chan model.NetworkAssignment,
	completions chan<- networkProbeCompletion,
) {
	defer workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-work:
			result := runner.options.Prober.Probe(ctx, task)
			completion := networkProbeCompletion{taskID: task.ID, result: result, report: ctx.Err() == nil}
			select {
			case completions <- completion:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (runner *NetworkRunner) reportResults(
	ctx context.Context,
	workers *sync.WaitGroup,
	results <-chan model.NetworkProbeResult,
) {
	defer workers.Done()
	ticker := time.NewTicker(runner.options.FlushInterval)
	defer ticker.Stop()
	batch := make([]model.NetworkProbeResult, 0, defaultResultQueueSize)
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			if len(batch) >= defaultResultQueueSize {
				copy(batch, batch[1:])
				batch = batch[:len(batch)-1]
				runner.options.OnError(fmt.Errorf("network result queue full: dropped oldest result"))
			}
			batch = append(batch, result)
			if len(batch) >= defaultResultBatchSize {
				batch = runner.flushResults(ctx, batch)
			}
		case <-ticker.C:
			batch = runner.flushResults(ctx, batch)
		}
	}
}

func (runner *NetworkRunner) flushResults(
	ctx context.Context,
	batch []model.NetworkProbeResult,
) []model.NetworkProbeResult {
	if len(batch) == 0 {
		return batch
	}
	count := min(len(batch), 64)
	if err := runner.options.Client.ReportResults(ctx, batch[:count]); err != nil {
		if ctx.Err() == nil {
			runner.options.OnError(fmt.Errorf("report network results: %w", err))
		}
		return batch
	}
	remaining := copy(batch, batch[count:])
	clear(batch[remaining:])
	return batch[:remaining]
}

func mergeNetworkAssignments(
	current map[string]*scheduledNetworkTask,
	set NetworkAssignmentSet,
) map[string]*scheduledNetworkTask {
	next := make(map[string]*scheduledNetworkTask, len(set.Tasks))
	now := model.NowUTC()
	for _, assignment := range set.Tasks {
		state := current[assignment.ID]
		if state == nil {
			state = &scheduledNetworkTask{next: now}
		}
		state.task = assignment
		state.expires = set.ExpiresAt
		next[assignment.ID] = state
	}
	return next
}

func enqueueDueNetworkTasks(
	tasks map[string]*scheduledNetworkTask,
	now time.Time,
	work chan<- model.NetworkAssignment,
) {
	for _, task := range tasks {
		if task.running || now.Before(task.next) || !now.Before(task.expires) {
			continue
		}
		select {
		case work <- task.task:
			task.running = true
			task.next = now.Add(time.Duration(task.task.IntervalSeconds) * time.Second)
		default:
			return
		}
	}
}
