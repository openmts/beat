package agent

import (
	"errors"
	"testing"
)

func TestDetectSSHPort(t *testing.T) {
	original := readSSHPort
	t.Cleanup(func() { readSSHPort = original })
	readSSHPort = func() (int, error) { return 2222, nil }
	port, err := DetectSSHPort()
	if err != nil || port != 2222 {
		t.Fatalf("want port 2222, got %d, %v", port, err)
	}
	failure := errors.New("read failed")
	readSSHPort = func() (int, error) { return 0, failure }
	if _, err := DetectSSHPort(); !errors.Is(err, failure) {
		t.Fatalf("want detection error, got %v", err)
	}
}
