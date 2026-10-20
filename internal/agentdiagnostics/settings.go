package agentdiagnostics

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

const (
	// heartbeatInterval 必须显著小于服务端 30 秒租约窗口，取消才能及时生效。
	heartbeatInterval = 10 * time.Second

	journalDirectory = "journal"
	workDirectory    = "work"

	capabilityStateReady   = "ready"
	capabilityStatePartial = "partial"
	capabilityKindCount    = 2
)

// settings 是校验并归一化后的本地运行时设置。
type settings struct {
	config    Config
	allowed   allowedSet
	executors map[string]executor
	journal   string
	work      string
	poll      time.Duration
	heartbeat time.Duration
}

func prepareSettings(input Config) (settings, error) {
	config := input.normalize()
	if err := ValidateConfig(config); err != nil {
		return settings{}, err
	}
	poll, err := time.ParseDuration(config.PollInterval)
	if err != nil {
		return settings{}, fmt.Errorf("%w: poll interval is invalid", ErrPolicy)
	}
	prepared := settings{config: config, poll: poll, heartbeat: heartbeatInterval}
	if !config.Enabled {
		return prepared, nil
	}
	allowed, err := newAllowedSet(config.AllowedTargets)
	if err != nil {
		return settings{}, err
	}
	prepared.allowed = allowed
	prepared.executors = configuredExecutors(config)
	prepared.journal = filepath.Join(config.StateDir, journalDirectory)
	prepared.work = filepath.Join(config.StateDir, workDirectory)
	return prepared, nil
}

func configuredExecutors(config Config) map[string]executor {
	executors := map[string]executor{}
	if config.MTRPath != "" {
		executors[model.DiagnosticKindMTR] = mtrExecutor{path: config.MTRPath}
	}
	if config.Iperf3Path != "" {
		executors[model.DiagnosticKindIperf3] = iperf3Executor{path: config.Iperf3Path}
	}
	return executors
}

// capability 生成上报文档；只有一种二进制时明确上报 partial。
func (settings settings) capability() diagnosticprotocol.CapabilityDocument {
	state := capabilityStatePartial
	if len(settings.executors) == capabilityKindCount {
		state = capabilityStateReady
	}
	return settings.config.Capability(state)
}
