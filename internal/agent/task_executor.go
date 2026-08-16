package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

const authorizedKeysFileMode = 0o600

// TaskExecutor dispatches agent tasks to their handlers.
type AuthorizedKeyExecutor struct {
	homeDir string
}

// NewAuthorizedKeyExecutor returns an executor that installs keys into the
// current user's authorized_keys. When homeDir is empty the user's home is
// resolved at execution time.
func NewAuthorizedKeyExecutor(homeDir string) *AuthorizedKeyExecutor {
	return &AuthorizedKeyExecutor{homeDir: homeDir}
}

func (e *AuthorizedKeyExecutor) ExecuteTask(ctx context.Context, task model.AgentTask) model.AgentTaskResult {
	if task.Type != model.AgentTaskAuthorizedKey {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "unsupported task type " + task.Type}
	}
	payload, err := model.DecodeAuthorizedKeyPayload(task.Payload)
	if err != nil {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: "invalid task payload"}
	}
	if err := installAuthorizedKey(e.homeDir, payload.PublicKey); err != nil {
		return model.AgentTaskResult{TaskID: task.ID, Success: false, Error: err.Error()}
	}
	return model.AgentTaskResult{TaskID: task.ID, Success: true}
}

func installAuthorizedKey(homeDir, publicKey string) error {
	key := strings.TrimSpace(publicKey)
	if key == "" || strings.ContainsAny(key, "\n\r") {
		return fmt.Errorf("invalid public key")
	}
	home := homeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("create ssh directory: %w", err)
	}
	if err := os.Chmod(sshDir, 0o700); err != nil {
		return fmt.Errorf("secure ssh directory: %w", err)
	}
	authorizedPath := filepath.Join(sshDir, "authorized_keys")
	if err := appendAuthorizedKey(authorizedPath, key); err != nil {
		return err
	}
	return os.Chmod(authorizedPath, authorizedKeysFileMode)
}

func appendAuthorizedKey(path, key string) error {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read authorized_keys: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == key {
			return nil
		}
	}
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		content = append(content, '\n')
	}
	content = append(content, []byte(key+"\n")...)
	if err := os.WriteFile(path, content, authorizedKeysFileMode); err != nil {
		return fmt.Errorf("write authorized_keys: %w", err)
	}
	return nil
}
