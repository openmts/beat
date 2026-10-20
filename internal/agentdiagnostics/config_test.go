package agentdiagnostics

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/model"
)

func validConfig(t *testing.T) Config {
	t.Helper()
	return Config{Enabled: true, MTRPath: "/usr/bin/mtr", StateDir: filepath.Join(t.TempDir(), "state"),
		PollInterval: "5s", AllowedTargets: []string{"8.8.8.8"},
		Maxima: Maxima{Probes: model.DefaultDiagnosticProbes, HopLimit: model.DefaultDiagnosticHops,
			DurationSeconds: model.DefaultIperfDurationSeconds, ParallelStreams: model.MinimumDiagnosticStreams,
			BitrateMbps: model.DefaultDiagnosticBitrateMbps}, MaximumConcurrent: 1}
}

func TestValidateConfigRejectsUnsafeSettings(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"no binary", func(config *Config) { config.MTRPath = "" }},
		{"relative binary", func(config *Config) { config.Iperf3Path = "bin/iperf3" }},
		{"no allowlist", func(config *Config) { config.AllowedTargets = nil }},
		{"hostname allowlist", func(config *Config) { config.AllowedTargets = []string{"example.com"} }},
		{"unmasked allowlist", func(config *Config) {
			config.AllowedTargets = []string{"10.0.0.1/8"}
			config.AllowPrivateTargets = true
		}},
		{"private allowlist without flag", func(config *Config) { config.AllowedTargets = []string{"10.0.0.0/8"} }},
		{"no state directory", func(config *Config) { config.StateDir = "" }},
		{"short poll interval", func(config *Config) { config.PollInterval = "100ms" }},
		{"long poll interval", func(config *Config) { config.PollInterval = "2h" }},
		{"concurrency too high", func(config *Config) { config.MaximumConcurrent = 3 }},
		{"probes out of range", func(config *Config) { config.Maxima.Probes = 1 }},
		{"hop limit out of range", func(config *Config) { config.Maxima.HopLimit = 0 }},
		{"duration out of range", func(config *Config) { config.Maxima.DurationSeconds = model.HardMaximumDiagnosticDurationSeconds + 1 }},
		{"streams out of range", func(config *Config) { config.Maxima.ParallelStreams = model.MaximumDiagnosticStreams + 1 }},
		{"bitrate out of range", func(config *Config) { config.Maxima.BitrateMbps = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := validConfig(t)
			testCase.mutate(&config)
			err := ValidateConfig(config)
			if !errors.Is(err, ErrPolicy) {
				t.Fatalf("ValidateConfig error = %v, want ErrPolicy", err)
			}
		})
	}
}

func TestValidateConfigAcceptsDisabledAndPrivateAllowlist(t *testing.T) {
	if err := ValidateConfig(Config{}); err != nil {
		t.Fatalf("disabled config error = %v", err)
	}
	config := validConfig(t)
	config.AllowedTargets = []string{"10.0.0.0/8", "2001:db8::/32"}
	config.AllowPrivateTargets = true
	config.Iperf3Path = "/usr/bin/iperf3"
	config.AllowUDP, config.AllowReverse = true, true
	if err := ValidateConfig(config); err != nil {
		t.Fatalf("enabled config error = %v", err)
	}
}

func TestCapabilityDocumentReflectsConfiguredBinaries(t *testing.T) {
	config := validConfig(t)
	document := config.Capability(capabilityStateReady)
	if document.State != capabilityStateReady || document.Version == 0 {
		t.Fatalf("capability document = %+v", document)
	}
	if _, err := document.Digest(); err != nil {
		t.Fatalf("digest error = %v", err)
	}
	if document.Iperf3 != nil {
		t.Fatal("iperf3 capability must be absent without iperf3_path")
	}
	config.Iperf3Path = "/usr/bin/iperf3"
	config.AllowUDP = true
	document = config.Capability(capabilityStateReady)
	if document.Iperf3 == nil || len(document.Iperf3.Protocols) != 2 || len(document.Kinds) != 2 {
		t.Fatalf("iperf3 capability = %+v", document.Iperf3)
	}
	if !document.Supports(model.DiagnosticKindIperf3, model.DiagnosticProtocolUDP, model.DiagnosticDirectionForward, 0, 0, 10, 1, 100) {
		t.Fatal("advertised capability does not support its own maxima")
	}
	if document.Supports(model.DiagnosticKindIperf3, model.DiagnosticProtocolTCP, model.DiagnosticDirectionReverse, 0, 0, 10, 1, 100) {
		t.Fatal("reverse direction must not be advertised by default")
	}
}
