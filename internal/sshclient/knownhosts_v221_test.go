package sshclient

import (
	"os"
	"testing"
)

func TestNewKnownHostsRequiresAPath(t *testing.T) {
	if _, err := NewKnownHosts(""); err == nil {
		t.Fatal("expected an empty known hosts path to be rejected")
	}
}

// /dev/full 是内核提供的恒返回 ENOSPC 的字符设备；用它证明 append 会把写入失败上报，
// 而不是静默丢弃新主机密钥。
func TestKnownHostsAppendReportsWriteFailure(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skipf("/dev/full is unavailable: %v", err)
	}
	knownHosts := &KnownHosts{path: "/dev/full"}
	if err := knownHosts.append("example.test:22", testPublicKey(t)); err == nil {
		t.Fatal("expected the write failure to be reported")
	}
}
