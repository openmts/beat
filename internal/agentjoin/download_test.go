package agentjoin

import (
	"strings"
	"testing"
)

func TestAgentDownloadCommand(t *testing.T) {
	command := AgentDownloadCommand("0.0.1-beta.6")
	for _, expected := range []string{
		`set -euo pipefail`,
		`x86_64|amd64) arch=amd64`,
		`aarch64|arm64) arch=arm64`,
		`https://github.com/openmts/beat/releases/download/v0.0.1-beta.6/beat-agent_linux_${arch}`,
		`chmod 0700 ./beat-agent`,
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("download command missing %q:\n%s", expected, command)
		}
	}
}

func TestAgentDownloadCommandAcceptsVPrefix(t *testing.T) {
	if !strings.Contains(AgentDownloadCommand("v1.2.3"), "/v1.2.3/") {
		t.Fatal("v-prefixed release version was not normalized")
	}
}

func TestAgentDownloadCommandOmitsUnpublishedVersions(t *testing.T) {
	for _, version := range []string{"", "0.2.0-dev", "0.2.0+worktree", "v1.2", "1.2.3/../../"} {
		if command := AgentDownloadCommand(version); command != "" {
			t.Fatalf("AgentDownloadCommand(%q) = %q, want empty", version, command)
		}
	}
}
