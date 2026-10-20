package agentoperations

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

type Runner struct {
	settings        settings
	gateway         Gateway
	journals        *journalStore
	engine          processEngine
	heartbeat, poll time.Duration
	mu              sync.Mutex
	active          map[string]string
	lastError       string
	onError         func(error)
	onEvent         func(Event)
	lifetime        context.Context
	stop            context.CancelFunc
	closed          bool
	running         sync.WaitGroup
	closeOnce       sync.Once
	closeErr        error
}

type RunnerConfig struct {
	Local      Config
	ConfigPath string
	Gateway    Gateway
	OnError    func(error)
	OnEvent    func(Event)
}

func NewRunner(config RunnerConfig) (*Runner, error) {
	settings, err := prepareSettings(config.Local, config.ConfigPath)
	if err != nil {
		return nil, err
	}
	runner := &Runner{settings: settings, gateway: config.Gateway, heartbeat: 5 * time.Second, poll: time.Second, active: map[string]string{}, onError: config.OnError, onEvent: config.OnEvent}
	runner.lifetime, runner.stop = context.WithCancel(context.Background())
	if !settings.config.Enabled {
		return runner, nil
	}
	if config.Gateway == nil {
		return nil, ErrTransport
	}
	if err := validateTransferRoots(settings.config); err != nil {
		return nil, err
	}
	engine, err := newEngine(settings)
	if err != nil {
		return nil, err
	}
	journals, err := openJournals(settings.config.StateDir)
	if err != nil {
		return nil, err
	}
	runner.engine, runner.journals = engine, journals
	return runner, nil
}

func (runner *Runner) Close() error {
	runner.closeOnce.Do(func() {
		runner.mu.Lock()
		runner.closed = true
		runner.stop()
		runner.mu.Unlock()
		runner.running.Wait()
		if runner.journals != nil {
			runner.closeErr = runner.journals.unlock()
		}
	})
	return runner.closeErr
}

func (runner *Runner) Capabilities() protocol.Policy { return runner.settings.policy }

func (runner *Runner) Run(ctx context.Context) error {
	if !runner.settings.config.Enabled {
		return nil
	}
	workers := sync.WaitGroup{}
	for range runner.settings.config.MaxConcurrency {
		workers.Go(func() { runner.loop(ctx) })
	}
	workers.Wait()
	return nil
}

func (runner *Runner) loop(ctx context.Context) {
	for ctx.Err() == nil && runner.lifetime.Err() == nil {
		runner.observe(runner.RunOnce(ctx))
		timer := time.NewTimer(runner.poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-runner.lifetime.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (runner *Runner) RunOnce(parent context.Context) error {
	ctx, cleanup := runner.executionContext(parent)
	defer cleanup()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !runner.settings.config.Enabled {
		return nil
	}
	if err := runner.Recover(ctx); err != nil {
		return err
	}
	lease, err := runner.gateway.Lease(ctx, runner.settings.policy)
	if err != nil || lease == nil {
		return err
	}
	if err := runner.validateLease(*lease); err != nil {
		return err
	}
	acknowledged, err := runner.journals.acknowledged(lease.AttemptID)
	if err != nil || acknowledged {
		return err
	}
	return runner.acceptLease(ctx, *lease)
}

func (runner *Runner) acceptLease(ctx context.Context, lease protocol.Lease) error {
	if !runner.admitKind(lease.AttemptID, lease.Payload.Limits.Kind) {
		return ErrPolicy
	}
	defer runner.release(lease.AttemptID)
	record, err := runner.journals.load(lease.AttemptID)
	if errors.Is(err, os.ErrNotExist) {
		record = journal{Version: 1, State: "leased", Lease: lease}
		record.Lease.Payload.Command = bytes.Clone(lease.Payload.Command)
		if err := runner.journals.save(record); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if record.Lease.Nonce != lease.Nonce {
		return ErrPolicy
	}
	return runner.resume(ctx, record)
}

func (runner *Runner) admit(id string) bool { return runner.admitKind(id, "shell") }

func (runner *Runner) admitKind(id, kind string) bool {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.closed || runner.active[id] != "" || len(runner.active) >= runner.settings.config.MaxConcurrency {
		return false
	}
	if kind == "deploy" || kind == "collect" {
		count := 0
		for _, activeKind := range runner.active {
			if activeKind == "deploy" || activeKind == "collect" {
				count++
			}
		}
		if count >= 2 {
			return false
		}
	}
	runner.active[id] = kind
	runner.running.Add(1)
	return true
}

func (runner *Runner) release(id string) {
	runner.mu.Lock()
	delete(runner.active, id)
	runner.mu.Unlock()
	runner.running.Done()
}

func (runner *Runner) validateLease(lease protocol.Lease) error {
	if lease.ExpiresAt <= time.Now().UnixMilli() {
		return ErrPolicy
	}
	return runner.validateLeasePayload(lease)
}

func (runner *Runner) validateLeasePayload(lease protocol.Lease) error {
	if !validAttemptID(lease.AttemptID) || len(lease.Nonce) != 64 {
		return ErrPolicy
	}
	if _, err := hex.DecodeString(lease.Nonce); err != nil {
		return ErrPolicy
	}
	if err := protocol.Authorize(runner.settings.policy, runner.settings.policy, lease.Payload.Limits); err != nil {
		return ErrPolicy
	}
	if isFileLease(lease) {
		return runner.validateFileLease(lease)
	}
	return runner.validateCommandLease(lease)
}

func (runner *Runner) validateCommandLease(lease protocol.Lease) error {
	if lease.Payload.File != nil || protocol.ValidateCommand(lease.Payload.Command) != nil {
		return ErrPolicy
	}
	if !shellSupported(lease.Payload.ShellFamily) {
		return ErrPolicy
	}
	family := lease.Payload.ShellFamily
	if family == "" {
		family = "sh"
	}
	if err := protocol.AuthorizeShell(runner.settings.policy, runner.settings.policy, family); err != nil {
		return ErrPolicy
	}
	if lease.Payload.Limits.RunAsRoot != (runner.settings.identity.uid == 0) {
		return ErrPolicy
	}
	return nil
}

func (runner *Runner) Recover(ctx context.Context) error {
	if runner.journals == nil {
		return nil
	}
	records, err := runner.journals.list()
	if err != nil {
		return err
	}
	for _, record := range records {
		if err := runner.recoverRecord(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (runner *Runner) recoverRecord(ctx context.Context, record journal) error {
	if !runner.admitKind(record.Lease.AttemptID, record.Lease.Payload.Limits.Kind) {
		return nil
	}
	defer runner.release(record.Lease.AttemptID)
	return runner.resume(ctx, record)
}

func (runner *Runner) executionContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(runner.lifetime, func() { cancel(context.Canceled) })
	if runner.lifetime.Err() != nil {
		cancel(context.Canceled)
	}
	return ctx, func() { _ = stop(); cancel(nil) }
}
