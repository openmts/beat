//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestMainStopsOnSignal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	content := `{
  "server_url": "http://127.0.0.1:1",
  "agent_token": "token",
  "node_name": "node",
  "advertised_host": "127.0.0.1",
  "ssh_port": 22,
  "report_interval": "1s"
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oldArgs := os.Args
	os.Args = []string{"beat-agent", "--config", path}
	t.Cleanup(func() { os.Args = oldArgs })
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	main()
}
