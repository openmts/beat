package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beat/backend/internal/model"
)

func TestAuthorizedKeyExecutorInstall(t *testing.T) {
	home := t.TempDir()
	executor := NewAuthorizedKeyExecutor(home)
	task := model.AgentTask{
		ID:      "t1",
		Type:    model.AgentTaskAuthorizedKey,
		Payload: `{"public_key":"ssh-ed25519 AAAAC3Nza"}`,
	}
	result := executor.ExecuteTask(context.Background(), task)
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}
	content, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if !strings.Contains(string(content), "ssh-ed25519 AAAAC3Nza") {
		t.Fatalf("authorized_keys = %q, want key present", content)
	}
	info, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("stat authorized_keys: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys mode = %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Join(home, ".ssh"))
	if err != nil {
		t.Fatalf("stat .ssh: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh mode = %v, want 0700", dirInfo.Mode().Perm())
	}
}

func TestAuthorizedKeyExecutorIdempotent(t *testing.T) {
	home := t.TempDir()
	executor := NewAuthorizedKeyExecutor(home)
	payload := `{"public_key":"ssh-ed25519 AAAAC3Nza"}`
	for i := 0; i < 3; i++ {
		result := executor.ExecuteTask(context.Background(), model.AgentTask{
			ID: "t1", Type: model.AgentTaskAuthorizedKey, Payload: payload,
		})
		if !result.Success {
			t.Fatalf("attempt %d failed: %#v", i+1, result)
		}
	}
	content, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatalf("read authorized_keys: %v", err)
	}
	if count := strings.Count(string(content), "ssh-ed25519 AAAAC3Nza"); count != 1 {
		t.Fatalf("key appears %d times, want exactly 1", count)
	}
}

func TestAuthorizedKeyExecutorUnsupportedType(t *testing.T) {
	executor := NewAuthorizedKeyExecutor(t.TempDir())
	result := executor.ExecuteTask(context.Background(), model.AgentTask{
		ID: "t1", Type: "bogus", Payload: `{"public_key":"ssh-ed25519 aaa"}`,
	})
	if result.Success || result.Error == "" {
		t.Fatalf("result = %#v, want failure with error", result)
	}
}

func TestAuthorizedKeyExecutorInvalidPayload(t *testing.T) {
	executor := NewAuthorizedKeyExecutor(t.TempDir())
	for _, payload := range []string{"", `{"public_key":""}`, `{"public_key":"multi\nline"}`} {
		result := executor.ExecuteTask(context.Background(), model.AgentTask{
			ID: "t1", Type: model.AgentTaskAuthorizedKey, Payload: payload,
		})
		if result.Success {
			t.Fatalf("payload %q should fail", payload)
		}
	}
}
