package agentinstall

import (
	"context"
	"runtime"
	"time"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentupdate"
	"github.com/beat/backend/internal/model"
)

func (installer *Installer) UpdateConfiguration(ctx context.Context, path string) (agentupdate.Config, error) {
	config, err := agent.LoadConfig(path)
	if err != nil {
		return agentupdate.Config{}, err
	}
	if config.Updates == nil {
		return agentupdate.Config{}, agentupdate.ErrDisabled
	}
	manager, err := installer.UpdateManager(ctx, path)
	if err != nil {
		return agentupdate.Config{}, err
	}
	keys, err := agentupdate.LoadTrust(installer.paths.binary)
	if err != nil {
		return agentupdate.Config{}, err
	}
	result := agentupdate.Config{Enabled: config.Updates.Enabled, Executable: installer.paths.binary, NodeID: config.Updates.NodeID,
		ServerURL: config.ServerURL, Keys: keys, Manager: manager, OS: installer.goos, Arch: runtime.GOARCH,
		ObservationWindow:     min(3*time.Minute, max(time.Minute, config.ReportInterval)),
		MaximumObservationGap: min(5*time.Minute, max(30*time.Second, 2*config.ReportInterval)),
		AgentProtocol:         model.AgentReportProtocol, Now: func() time.Time { return time.Now().UTC() }}
	result.Installed, err = agentupdate.ResolveInstalled(ctx, result)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (installer *Installer) BuildUpdater(ctx context.Context, path string) (*agentupdate.Updater, error) {
	config, err := installer.UpdateConfiguration(ctx, path)
	if err != nil {
		return nil, err
	}
	if err = config.Manager.(*updateManager).Ready(ctx); err != nil {
		return nil, err
	}
	if err = agentupdate.CheckHelper(ctx, config); err != nil {
		return nil, err
	}
	return agentupdate.New(config)
}

func (installer *Installer) BuildUpdateHelper(ctx context.Context, path string) (*agentupdate.Updater, error) {
	config, err := installer.UpdateConfiguration(ctx, path)
	if err != nil {
		return nil, err
	}
	if err = agentupdate.VerifyHelper(ctx, config); err != nil {
		return nil, err
	}
	current, err := agent.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	fetcher, err := agentupdate.NewHTTPFetcher(agentupdate.HTTPConfig{ServerURL: current.ServerURL, AgentToken: current.AgentToken})
	if err != nil {
		return nil, err
	}
	config.Fetcher = fetcher
	updater, err := agentupdate.New(config)
	if err != nil {
		fetcher.Close()
	}
	return updater, err
}
