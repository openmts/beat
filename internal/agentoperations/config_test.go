//go:build linux

package agentoperations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beat/backend/internal/remoteoperations/protocol"
)

func TestLocalPolicyRejectsUnsafeAuthorityAndRoots(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.MaxConcurrency = 5 },
		func(c *Config) { c.Commands.MaximumTimeout = "bad" },
		func(c *Config) { c.Commands.MaximumTimeout = "1ms" },
		func(c *Config) { c.Commands.MaximumOutputBytes = protocol.MaximumOutputBytes + 1 },
		func(c *Config) { c.StateDir = "relative" },
		func(c *Config) { c.Commands.MemoryBytes = 1 },
		func(c *Config) { c.Commands.MaximumProcesses = 2000 },
		func(c *Config) { c.Commands.RunAsUser = "beat-no-such-user-fixture" },
		func(c *Config) { c.Commands.WorkingRoot = "missing" },
		func(c *Config) { c.FileRoots[0].Path = "relative" },
		func(c *Config) { c.FileRoots[0].Path = c.StateDir },
		func(c *Config) { c.FileRoots[0].Path = "/proc" },
		func(c *Config) { c.FileRoots[0].Path = "/" },
		func(c *Config) { c.FileRoots = append(c.FileRoots, c.FileRoots[0]) },
		func(c *Config) { root := c.FileRoots[0]; root.ID = "second"; c.FileRoots = append(c.FileRoots, root) },
	} {
		config := testConfig(t)
		change(&config)
		if err := ValidateConfig(config, ""); err == nil {
			t.Fatal("unsafe local configuration accepted")
		}
	}
	config := testConfig(t)
	if err := ValidateConfig(config, filepath.Join(config.FileRoots[0].Path, "agent.json")); err == nil {
		t.Fatal("operations root overlaps credentials")
	}
	if err := os.Chmod(config.FileRoots[0].Path, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfig(config, ""); err == nil {
		t.Fatal("world-writable root accepted")
	}
	if os.Geteuid() == 0 {
		config = testConfig(t)
		config.Commands.AllowRoot = false
		if err := ValidateConfig(config, ""); err == nil {
			t.Fatal("implicit root execution enabled")
		}
	}
}

func TestLeaseValidationNeverExpandsLocalPolicy(t *testing.T) {
	runner, _ := testRunner(t, &fakeGateway{})
	for _, change := range []func(*protocol.Lease){
		func(l *protocol.Lease) { l.AttemptID = "../escape" },
		func(l *protocol.Lease) { l.Nonce = "short" },
		func(l *protocol.Lease) { l.Nonce = "zz" + l.Nonce[2:] },
		func(l *protocol.Lease) { l.ExpiresAt = 1 },
		func(l *protocol.Lease) { l.Payload.Command = nil },
		func(l *protocol.Lease) { l.Payload.Limits.OutputBytes = protocol.MaximumOutputBytes },
		func(l *protocol.Lease) { l.Payload.ShellFamily = "powershell" },
		func(l *protocol.Lease) { l.Payload.Limits.RunAsRoot = !l.Payload.Limits.RunAsRoot },
		func(l *protocol.Lease) { l.Payload.Limits.Kind = "deploy" },
		func(l *protocol.Lease) { l.Payload.File = &protocol.FileSpec{} },
	} {
		lease := testLease("true")
		change(&lease)
		if err := runner.validateLease(lease); err == nil {
			t.Fatal("lease widened local policy")
		}
	}
}

func TestDisabledPolicyNeedsNoAccountOrStateDirectory(t *testing.T) {
	if err := ValidateConfig(Config{}, ""); err != nil {
		t.Fatal(err)
	}
	runner, err := NewRunner(RunnerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Capabilities().Enabled || len(runner.Capabilities().Kinds) != 0 {
		t.Fatal("disabled capability enabled tasks")
	}
	config := testConfig(t)
	if _, err := NewRunner(RunnerConfig{Local: config}); err == nil {
		t.Fatal("missing gateway accepted")
	}
	config.Commands.RequireCgroup = nil
	if _, err := NewRunner(RunnerConfig{Local: config, Gateway: &fakeGateway{}}); err == nil {
		t.Fatal("missing containment accepted")
	}
}
