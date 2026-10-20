package agentinstall

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/beat/backend/internal/agentjoin"
)

func (installer *Installer) Join(ctx context.Context, options JoinOptions) (summary Summary, err error) {
	options, err = installer.normalizeOptions(options)
	if err != nil {
		return Summary{}, err
	}
	release, err := installer.begin(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer func() { err = errors.Join(err, release()) }()
	state, err := installer.enrollment(options)
	if err != nil {
		return Summary{}, err
	}
	if err := installer.checkConfig(state); err != nil {
		return Summary{}, err
	}
	unit := unitFile(installer.paths.binary, installer.paths.config)
	if err := installer.checkSystemd(ctx, unit); err != nil {
		return Summary{}, err
	}
	state, err = installer.finishEnrollment(ctx, options, state)
	if err != nil {
		return Summary{}, err
	}
	if err := installer.completeInstall(ctx, state); err != nil {
		return Summary{}, fmt.Errorf("node enrollment is saved; retry this command or restore locally with service install --config %s (no join token required): %w", installer.paths.config, err)
	}
	return Summary{NodeID: state.Result.NodeID, ServerURL: options.ServerURL, ConfigPath: installer.paths.config}, nil
}

func (installer *Installer) normalizeOptions(options JoinOptions) (JoinOptions, error) {
	server, err := agentjoin.NormalizeServerURL(options.ServerURL, options.AllowHTTP)
	if err != nil {
		return JoinOptions{}, err
	}
	if err := agentjoin.ValidateToken(options.Token); err != nil {
		return JoinOptions{}, err
	}
	options.ServerURL = server
	if options.Name == "" {
		options.Name, err = installer.hostname()
		if err != nil {
			return JoinOptions{}, fmt.Errorf("detect hostname: %w", err)
		}
	}
	options.Name, options.Host = strings.TrimSpace(options.Name), strings.TrimSpace(options.Host)
	if options.SSHPort == 0 {
		options.SSHPort, err = installer.sshPort()
		if err != nil {
			return JoinOptions{}, fmt.Errorf("detect SSH port: %w", err)
		}
	}
	return options, nil
}

func (installer *Installer) finishEnrollment(ctx context.Context, options JoinOptions, state *enrollment) (*enrollment, error) {
	plan, err := installer.planBinary(installer.paths.config)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state, err = installer.createEnrollment(options)
		if err != nil {
			return nil, err
		}
	}
	if err := installer.installBinary(plan); err != nil {
		return nil, err
	}
	if state.Result != nil {
		return state, nil
	}
	result, err := installer.exchange(ctx, joinExchange{options.ServerURL, options.Token, state.Request})
	if err != nil {
		return nil, installer.recordJoinError(state, err)
	}
	state.Result, state.RenameAllowed = &result, false
	if err := validateResult(*state); err != nil {
		return nil, err
	}
	if err := installer.saveEnrollment(state); err != nil {
		return nil, fmt.Errorf("save join receipt; rerun the same command to recover: %w", err)
	}
	return state, nil
}

func (installer *Installer) completeInstall(ctx context.Context, state *enrollment) error {
	if err := installer.saveJSON(installer.paths.config, state.Result.Config); err != nil {
		return err
	}
	return installer.startService(ctx, unitFile(installer.paths.binary, installer.paths.config))
}

func (installer *Installer) recordJoinError(state *enrollment, err error) error {
	if !errors.Is(err, errNodeNameConflict) {
		return err
	}
	state.RenameAllowed = true
	return errors.Join(err, installer.saveEnrollment(state))
}
