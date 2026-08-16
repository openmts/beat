package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// AuthorizedKeySelfHealer ensures the node's assigned SSH public key is present
// in the local authorized_keys file, repairing drift caused by external edits.
type AuthorizedKeySelfHealer struct {
	homeDir string
}

// NewAuthorizedKeySelfHealer returns a self-healer for the current user's
// authorized_keys. When homeDir is empty the user's home is resolved at
// execution time.
func NewAuthorizedKeySelfHealer(homeDir string) *AuthorizedKeySelfHealer {
	return &AuthorizedKeySelfHealer{homeDir: homeDir}
}

// SelfHeal installs the assigned public key when it is missing locally.
func (h *AuthorizedKeySelfHealer) SelfHeal(ctx context.Context, publicKey string) error {
	key := strings.TrimSpace(publicKey)
	if key == "" || strings.ContainsAny(key, "\n\r") {
		return nil
	}
	if h.authorizedKeysContains(key) {
		return nil
	}
	return installAuthorizedKey(h.homeDir, key)
}

func (h *AuthorizedKeySelfHealer) authorizedKeysContains(key string) bool {
	home := h.homeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return false
		}
	}
	content, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == key {
			return true
		}
	}
	return false
}
