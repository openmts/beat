// Package agentdiagnostics 是 Agent 侧的高级诊断执行运行时。
//
// 它只执行运维显式批准的绝对二进制与固定参数，从不继承调用方环境、不使用
// shell、不接受任意命令行，并且本地上限与服务端上限取交集。
package agentdiagnostics

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/beat/backend/internal/diagnosticprotocol"
	"github.com/beat/backend/internal/model"
)

var (
	ErrPolicy    = errors.New("agent diagnostic policy rejected the request")
	ErrStorage   = errors.New("agent diagnostic state is unavailable")
	ErrTransport = errors.New("agent diagnostic protocol request was not acknowledged")
	ErrExecution = errors.New("agent diagnostic execution failed")
	ErrCancelled = errors.New("agent diagnostic execution was cancelled")
)

const (
	defaultPollInterval = 5 * time.Second
	minimumConcurrency  = 1
	maximumConcurrency  = 2
)

// Maxima 是 Agent 本地上限；服务端只能收紧它们。
type Maxima struct {
	Probes          int `json:"probes"`
	HopLimit        int `json:"hop_limit"`
	DurationSeconds int `json:"duration_seconds"`
	ParallelStreams int `json:"parallel_streams"`
	BitrateMbps     int `json:"bitrate_mbps"`
}

// Config 是 Agent 本地诊断配置；默认关闭，并要求运维显式列出允许目标。
type Config struct {
	MTRPath             string   `json:"mtr_path"`
	Iperf3Path          string   `json:"iperf3_path"`
	Iperf3Version       string   `json:"iperf3_version"`
	MTRVersion          string   `json:"mtr_version"`
	StateDir            string   `json:"state_dir"`
	PollInterval        string   `json:"poll_interval"`
	AllowedTargets      []string `json:"allowed_targets"`
	Maxima              Maxima   `json:"maxima"`
	MaximumConcurrent   int      `json:"maximum_concurrent"`
	Enabled             bool     `json:"enabled"`
	AllowUDP            bool     `json:"allow_udp"`
	AllowReverse        bool     `json:"allow_reverse"`
	AllowPrivateTargets bool     `json:"allow_private_targets"`
}

func (config Config) normalize() Config {
	if config.PollInterval == "" {
		config.PollInterval = defaultPollInterval.String()
	}
	if config.MaximumConcurrent == 0 {
		config.MaximumConcurrent = minimumConcurrency
	}
	if config.Maxima == (Maxima{}) {
		config.Maxima = Maxima{Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops,
			DurationSeconds: model.DefaultIperfDurationSeconds, ParallelStreams: model.MinimumDiagnosticStreams,
			BitrateMbps: model.DefaultDiagnosticBitrateMbps}
	}
	return config
}

// Validate 校验本地配置；启用时必须具备可执行二进制、私有状态目录与目标白名单。
func ValidateConfig(config Config) error {
	config = config.normalize()
	if interval, err := time.ParseDuration(config.PollInterval); err != nil || interval < time.Second || interval > time.Hour {
		return fmt.Errorf("%w: poll interval is invalid", ErrPolicy)
	}
	if config.MaximumConcurrent < minimumConcurrency || config.MaximumConcurrent > maximumConcurrency {
		return fmt.Errorf("%w: concurrency must be %d-%d", ErrPolicy, minimumConcurrency, maximumConcurrency)
	}
	if err := config.Maxima.validate(); err != nil {
		return err
	}
	if !config.Enabled {
		return nil
	}
	if config.MTRPath == "" && config.Iperf3Path == "" {
		return fmt.Errorf("%w: at least one diagnostic binary is required", ErrPolicy)
	}
	if len(config.AllowedTargets) == 0 || len(config.AllowedTargets) > model.MaximumDiagnosticTargetAddresses {
		return fmt.Errorf("%w: an explicit target allowlist is required", ErrPolicy)
	}
	for _, entry := range config.AllowedTargets {
		if err := validateAllowlistEntry(entry); err != nil {
			return err
		}
		if err := validateAllowlistPrivacy(entry, config.AllowPrivateTargets); err != nil {
			return err
		}
	}
	if config.StateDir == "" {
		return fmt.Errorf("%w: state directory is required", ErrPolicy)
	}
	if config.MTRPath != "" && !validBinaryPath(config.MTRPath) {
		return fmt.Errorf("%w: mtr path must be an absolute binary", ErrPolicy)
	}
	if config.Iperf3Path != "" && !validBinaryPath(config.Iperf3Path) {
		return fmt.Errorf("%w: iperf3 path must be an absolute binary", ErrPolicy)
	}
	return nil
}

// validateAllowlistPrivacy 要求私网白名单与 allow_private_targets 同时显式开启，
// 避免配置看起来允许私网目标、实际执行时又全部被拒绝。
func validateAllowlistPrivacy(entry string, privateAllowed bool) error {
	prefix, err := parsePrefix(entry)
	if err != nil {
		return err
	}
	if diagnosticprotocol.RestrictedPrefix(prefix) && !privateAllowed {
		return fmt.Errorf("%w: allowlist entry %q needs allow_private_targets", ErrPolicy, entry)
	}
	return nil
}

func (maxima Maxima) validate() error {
	if maxima.Probes < model.MinimumDiagnosticProbes || maxima.Probes > model.MaximumDiagnosticProbes {
		return fmt.Errorf("%w: probes are out of range", ErrPolicy)
	}
	if maxima.HopLimit < 1 || maxima.HopLimit > model.MaximumDiagnosticHops {
		return fmt.Errorf("%w: hop limit is out of range", ErrPolicy)
	}
	if maxima.DurationSeconds < model.MinimumDiagnosticDurationSeconds ||
		maxima.DurationSeconds > model.HardMaximumDiagnosticDurationSeconds {
		return fmt.Errorf("%w: duration is out of range", ErrPolicy)
	}
	if maxima.ParallelStreams < model.MinimumDiagnosticStreams || maxima.ParallelStreams > model.MaximumDiagnosticStreams {
		return fmt.Errorf("%w: parallel streams are out of range", ErrPolicy)
	}
	if maxima.BitrateMbps < 1 || maxima.BitrateMbps > model.MaximumDiagnosticBitrateMbps {
		return fmt.Errorf("%w: bitrate is out of range", ErrPolicy)
	}
	return nil
}

// Capability 生成能力文档；缺失的二进制对应能力直接不出现。
func (config Config) Capability(state string) diagnosticprotocol.CapabilityDocument {
	config = config.normalize()
	document := diagnosticprotocol.CapabilityDocument{Version: diagnosticprotocol.Version, State: state,
		Maxima: diagnosticprotocol.CapabilityMaxima{Probes: config.Maxima.Probes, HopLimit: config.Maxima.HopLimit,
			DurationSeconds: config.Maxima.DurationSeconds, ParallelStreams: config.Maxima.ParallelStreams,
			BitrateMbps: config.Maxima.BitrateMbps}}
	families := []string{model.IPFamilyIPv4, model.IPFamilyIPv6}
	if config.MTRPath != "" {
		document.Kinds = append(document.Kinds, model.DiagnosticKindMTR)
		document.MTR = &diagnosticprotocol.MTRCapability{Binary: config.MTRPath, Version: config.MTRVersion,
			IPFamilies: families, Protocols: config.mtrProtocols()}
	}
	if config.Iperf3Path != "" {
		document.Kinds = append(document.Kinds, model.DiagnosticKindIperf3)
		document.Iperf3 = &diagnosticprotocol.Iperf3Capability{Binary: config.Iperf3Path,
			Version: config.Iperf3Version, Protocols: config.streamProtocols(), Directions: config.directions()}
	}
	return document
}

func (config Config) mtrProtocols() []string {
	protocols := []string{model.DiagnosticProtocolICMP, model.DiagnosticProtocolTCP}
	if config.AllowUDP {
		protocols = append(protocols, model.DiagnosticProtocolUDP)
	}
	return protocols
}

func (config Config) streamProtocols() []string {
	protocols := []string{model.DiagnosticProtocolTCP}
	if config.AllowUDP {
		protocols = append(protocols, model.DiagnosticProtocolUDP)
	}
	return protocols
}

func (config Config) directions() []string {
	directions := []string{model.DiagnosticDirectionForward}
	if config.AllowReverse {
		directions = append(directions, model.DiagnosticDirectionReverse)
	}
	return directions
}

func validBinaryPath(path string) bool {
	if !strings.HasPrefix(path, "/") || len(path) > 260 || strings.ContainsAny(path, "\x00\n\r") {
		return false
	}
	return !slices.Contains(strings.Split(path, "/"), "..")
}
