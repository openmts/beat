package agentupdate

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/beat/backend/internal/agentrollout/protocol"
)

type Controller struct {
	RunningVersion                             string
	pending                                    *protocol.Assignment
	taskIDs                                    map[string]struct{}
	processID                                  string
	Build                                      func(context.Context) (*Updater, error)
	Client                                     *ControlClient
	OnError                                    func(error)
	PollInterval                               time.Duration
	mu                                         sync.Mutex
	report                                     *acceptedReport
	protocolErrors, metricErrors, taskFailures int
	updater                                    *Updater
	nextPoll                                   time.Time
}

type acceptedReport struct{ serverAt, receivedAt time.Time }

// ReportAccepted仅由完整有效报告ACK触发；timer不能制造健康样本。
func (controller *Controller) ReportAccepted(serverAt time.Time) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.report = &acceptedReport{serverAt: serverAt, receivedAt: time.Now().UTC()}
}

func (controller *Controller) ReportFailed(protocolFailure bool) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if protocolFailure {
		controller.protocolErrors++
	} else {
		controller.metricErrors++
	}
}

func (controller *Controller) TaskFailed(id string) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.taskIDs == nil {
		controller.taskIDs = make(map[string]struct{})
	}
	if _, seen := controller.taskIDs[id]; seen {
		return
	}
	if len(controller.taskIDs) < maximumLocalFailures {
		controller.taskIDs[id] = struct{}{}
		controller.taskFailures++
	}
}

func (controller *Controller) Run(ctx context.Context) error {
	if controller.Build == nil || controller.Client == nil {
		return ErrConfiguration
	}
	defer controller.Client.Close()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if err := controller.step(ctx); err != nil && !errors.Is(err, ErrBusy) && controller.OnError != nil {
				controller.OnError(err)
			}
			timer.Reset(time.Second)
		}
	}
}

func (controller *Controller) step(ctx context.Context) error {
	if controller.updater == nil {
		if err := controller.initialize(ctx); err != nil {
			return err
		}
	}
	return errors.Join(controller.observe(ctx), controller.poll(ctx))
}

func (controller *Controller) poll(ctx context.Context) error {
	if controller.pending != nil {
		return controller.enqueuePending(ctx)
	}
	now := time.Now()
	if now.Before(controller.nextPoll) {
		return nil
	}
	interval := controller.PollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	controller.nextPoll = now.Add(interval)
	u := controller.updater
	if manager, ok := u.config.Manager.(interface{ Ready(context.Context) error }); ok {
		if err := manager.Ready(ctx); err != nil {
			return err
		}
	}
	caps, err := u.Capabilities()
	if err != nil {
		return err
	}
	assignment, err := controller.Client.Lease(ctx, caps)
	if err != nil || assignment == nil {
		return err
	}
	controller.pending = assignment
	return controller.enqueuePending(ctx)
}

func (controller *Controller) observe(ctx context.Context) error {
	controller.mu.Lock()
	report := controller.report
	controller.report = nil
	counts := protocol.Observation{ProtocolErrors: controller.protocolErrors, MetricErrors: controller.metricErrors, TaskFailures: controller.taskFailures}
	controller.mu.Unlock()
	if report == nil {
		return nil
	}
	u := controller.updater
	_, observeErr := u.Observe(ctx, HealthEvidence{ProcessID: controller.processID, Version: controller.RunningVersion, ConfigParsed: true, IdentityLoaded: true, ServerAuthenticated: true, ValidSample: true, At: report.receivedAt})
	if observeErr != nil && !errors.Is(observeErr, ErrHealth) {
		return observeErr
	}
	observation, err := u.Observation(report.serverAt)
	if err != nil || observation == nil {
		return err
	}
	if observeErr != nil && observation.Outcome == "healthy" {
		return observeErr
	}
	observation.ProtocolErrors, observation.MetricErrors, observation.TaskFailures = counts.ProtocolErrors, counts.MetricErrors, counts.TaskFailures
	return controller.Client.Observe(ctx, *observation)
}

func (u *Updater) Capabilities() (protocol.Capabilities, error) {
	caps := protocol.Capabilities{CurrentVersion: u.config.Installed.Version, OS: u.config.OS, Arch: u.config.Arch, ProtocolVersion: u.config.AgentProtocol}
	if err := u.managementGate(); err != nil {
		return caps, err
	}
	caps.Manager = u.config.Manager.Kind()
	caps.UpdateEnabled = u.config.Enabled
	inst, err := openInstallation(u.config.Executable, false)
	if err != nil {
		return caps, err
	}
	defer inst.close()
	space, err := availableSpace(inst.root)
	if err != nil {
		return caps, err
	}
	const maximumCapacity = 1<<63 - 1
	caps.AvailableBytes = int64(min(space, uint64(maximumCapacity)))
	return caps, nil
}

func (controller *Controller) enqueuePending(ctx context.Context) error {
	err := controller.updater.Enqueue(*controller.pending)
	if err == nil {
		controller.pending = nil
		return nil
	}
	if errors.Is(err, ErrBusy) {
		return err
	}
	if !errors.Is(err, ErrAssignment) && !errors.Is(err, ErrVersion) && !errors.Is(err, ErrManualOnly) {
		return err
	}
	assignment := controller.pending
	observation := protocol.Observation{AssignmentID: assignment.ID, Nonce: assignment.Nonce, Version: controller.updater.config.Installed.Version, ReportAt: controller.updater.config.Now(), Outcome: "failed"}
	reportErr := controller.Client.Observe(ctx, observation)
	if reportErr == nil || !controller.updater.config.Now().Before(assignment.ExpiresAt) {
		controller.pending = nil
	}
	return errors.Join(err, reportErr)
}

func (controller *Controller) initialize(ctx context.Context) error {
	updater, err := controller.Build(ctx)
	if err != nil {
		return err
	}
	if err = updater.VerifyRunningImage(ctx); err != nil {
		return err
	}
	if !protocol.SameReleaseVersion(controller.RunningVersion, updater.config.Installed.Version) {
		return ErrVersion
	}
	controller.processID, err = processIdentity()
	if err != nil {
		return err
	}
	controller.updater = updater
	return nil
}
