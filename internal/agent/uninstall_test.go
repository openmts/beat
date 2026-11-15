package agent

import "testing"

func TestAllowedAgentBinary(t *testing.T) {
	for _, path := range []string{
		"/usr/local/bin/beat-agent",
	} {
		if !allowedAgentBinary(path) {
			t.Fatalf("allowedAgentBinary(%q) = false", path)
		}
	}
	for _, path := range []string{
		"/root/projects/beat/bin/beat-agent",
		"/tmp/beat-agent",
		"/usr/bin/beat-agent",
		"/opt/beat/beat-agent",
		"/usr/local/lib/beat-agent",
	} {
		if allowedAgentBinary(path) {
			t.Fatalf("allowedAgentBinary(%q) = true", path)
		}
	}
}
