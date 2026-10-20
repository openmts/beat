package main

import (
	"log/slog"

	"github.com/beat/backend/internal/agent"
	"github.com/beat/backend/internal/agentdiagnostics"
)

// newDiagnostics 只在配置显式开启时构造诊断运行时；关闭时返回 nil。
func newDiagnostics(config agent.Config) (*agentdiagnostics.Runner, error) {
	if config.Diagnostics == nil || !config.Diagnostics.Enabled {
		return nil, nil
	}
	client, err := agentdiagnostics.NewClient(agentdiagnostics.ClientConfig{ServerURL: config.ServerURL, Token: config.AgentToken})
	if err != nil {
		return nil, err
	}
	return agentdiagnostics.NewRunner(agentdiagnostics.RunnerConfig{Local: *config.Diagnostics, Gateway: client,
		OnError: func(err error) { slog.Warn("agent diagnostics unavailable", "code", agentdiagnostics.ErrorCode(err)) }})
}
