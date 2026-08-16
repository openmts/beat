package agent

import (
	"os"
	"testing"
)

func TestSystemSSHPortResolution(t *testing.T) {
	if _, err := os.Stat(systemSSHConfigPath); os.IsNotExist(err) {
		t.Skipf("%s not present", systemSSHConfigPath)
	}
	port, err := readSSHPort()
	if err != nil {
		t.Fatalf("read ssh port: %v", err)
	}
	direct, err := readSSHPortFromFile(systemSSHConfigPath)
	if err != nil {
		t.Fatalf("read ssh port from file: %v", err)
	}
	if port != direct {
		t.Fatalf("resolved port %d != direct read %d", port, direct)
	}
	if port < 1 || port > 65535 {
		t.Fatalf("invalid resolved ssh port: %d", port)
	}
}
