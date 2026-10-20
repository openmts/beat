package agentupdate

import (
	"context"
	"errors"
	"time"
)

// Supervisor必须运行在与Agent服务不同的cgroup中。
type Supervisor struct {
	lastReported string
	Build        func(context.Context) (*Updater, error)
	OnError      func(error)
	Interval     time.Duration
}

func (supervisor *Supervisor) Run(ctx context.Context) error {
	if supervisor.Build == nil {
		return ErrConfiguration
	}
	interval := supervisor.Interval
	if interval <= 0 {
		interval = time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			err := supervisor.step(ctx)
			if err != nil && !errors.Is(err, ErrBusy) && supervisor.OnError != nil {
				supervisor.OnError(err)
			}
			timer.Reset(interval)
		}
	}
}

func (supervisor *Supervisor) step(ctx context.Context) error {
	updater, err := supervisor.Build(ctx)
	if err != nil {
		return err
	}
	if closer, ok := updater.config.Fetcher.(interface{ Close() }); ok {
		defer closer.Close()
	}
	status, err := updater.Recover(ctx)
	if err != nil {
		return err
	}
	if status.Phase == "rolled_back" {
		if err = supervisor.reportRollback(ctx, updater); err != nil {
			return err
		}
	}
	if !readyForAssignment(updater, status) {
		return nil
	}
	assignment, err := updater.Pending()
	if err != nil || assignment == nil {
		return err
	}
	return supervisor.applyPending(ctx, updater, *assignment)
}

func (supervisor *Supervisor) reportRollback(ctx context.Context, updater *Updater) error {
	observation, err := updater.Observation(updater.config.Now())
	if err != nil || observation == nil {
		return err
	}
	if observation.AssignmentID == supervisor.lastReported {
		return nil
	}
	fetcher, ok := updater.config.Fetcher.(*HTTPFetcher)
	if !ok {
		return nil
	}
	client := &ControlClient{fetcher: fetcher}
	if err = client.Observe(ctx, *observation); err != nil {
		return err
	}
	supervisor.lastReported = observation.AssignmentID
	return nil
}

func readyForAssignment(updater *Updater, status Status) bool {
	return updater.config.Enabled && (terminalPhase(status.Phase) || prepublication(status.Phase) || status.Phase == "idle")
}
