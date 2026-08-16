package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfHealInstallsMissingKey(t *testing.T) {
	home := t.TempDir()
	healer := NewAuthorizedKeySelfHealer(home)
	if err := healer.SelfHeal(context.Background(), "ssh-ed25519 AAA"); err != nil {
		t.Fatalf("self heal: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if !strings.Contains(string(content), "ssh-ed25519 AAA") {
		t.Fatalf("authorized_keys = %q, want key present", content)
	}
}

func TestSelfHealSkipsExistingKey(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	key := "ssh-ed25519 AAAAC3"
	if err := os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte(key+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(sshDir, "authorized_keys"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	healer := NewAuthorizedKeySelfHealer(home)
	if err := healer.SelfHeal(context.Background(), key); err != nil {
		t.Fatalf("self heal: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(sshDir, "authorized_keys"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("authorized_keys should be unchanged when key already present")
	}
}

func TestSelfHealIgnoresInvalidKey(t *testing.T) {
	healer := NewAuthorizedKeySelfHealer(t.TempDir())
	for _, key := range []string{"", "  ", "multi\nline"} {
		if err := healer.SelfHeal(context.Background(), key); err != nil {
			t.Fatalf("self heal with %q should not error: %v", key, err)
		}
	}
}

func TestSelfHealUsesHomeDirWhenEmpty(t *testing.T) {
	original, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home dir: %v", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	// os.UserHomeDir respects $HOME on unix; force it via a temp home.
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set home: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("HOME", original) })

	healer := NewAuthorizedKeySelfHealer("")
	if err := healer.SelfHeal(context.Background(), "ssh-ed25519 BBB"); err != nil {
		t.Fatalf("self heal: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if !strings.Contains(string(content), "ssh-ed25519 BBB") {
		t.Fatalf("authorized_keys = %q", content)
	}
}
