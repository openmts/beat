package agent

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

func TestSystemSSHPortResolution(t *testing.T) {
	if _, err := os.Stat(systemSSHConfigPath); os.IsNotExist(err) {
		t.Skipf("%s not present", systemSSHConfigPath)
	}
	port, err := readSSHPort()
	if errors.Is(err, fs.ErrPermission) {
		t.Skipf("system SSH configuration requires a privileged test runner: %v", err)
	}
	if err != nil {
		t.Fatalf("read ssh port: %v", err)
	}
	direct, err := readSSHPortFromFile(systemSSHConfigPath)
	if errors.Is(err, fs.ErrPermission) {
		t.Skipf("system SSH configuration requires a privileged test runner: %v", err)
	}
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
