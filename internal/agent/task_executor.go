package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/beat/backend/internal/model"
)

const authorizedKeysFileMode = 0o600

// authorizedKeysMu 串行化同一 Agent 进程内的读改写；自愈与 WebSocket 任务
// 会并发更新同一个文件，缺少串行化时后写者可能覆盖先写者的公钥。
var authorizedKeysMu sync.RWMutex

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
	authorizedKeysMu.Lock()
	defer authorizedKeysMu.Unlock()
	if err := appendAuthorizedKey(authorizedPath, key); err != nil {
		return err
	}
	return nil
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
	return replaceAuthorizedKeys(path, content)
}

// replaceAuthorizedKeys 使用同目录临时文件原子替换，避免进程中断留下截断文件。
func replaceAuthorizedKeys(path string, content []byte) (resultErr error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".authorized_keys-*")
	if err != nil {
		return fmt.Errorf("create authorized_keys temporary file: %w", err)
	}
	temporary := file.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(authorizedKeysFileMode); err != nil {
		return errors.Join(fmt.Errorf("secure temporary authorized_keys: %w", err), file.Close())
	}
	if _, err := file.Write(content); err != nil {
		return errors.Join(fmt.Errorf("write temporary authorized_keys: %w", err), file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(fmt.Errorf("sync temporary authorized_keys: %w", err), file.Close())
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary authorized_keys: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace authorized_keys: %w", err)
	}
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
