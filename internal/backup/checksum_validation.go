package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beat/backend/internal/model"
)

func validateChecksumList(root string, manifest model.BackupManifest) error {
	content, err := os.ReadFile(filepath.Join(root, checksumsEntry))
	if err != nil {
		return fmt.Errorf("read checksum list: %w", err)
	}
	if len(content) > 64<<10 {
		return errors.New("checksum list is too large")
	}
	parsed := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || parsed[parts[1]] != "" {
			return errors.New("checksum list is malformed")
		}
		parsed[parts[1]] = parts[0]
	}
	if len(parsed) != len(manifest.Checksums) {
		return errors.New("checksum list does not match manifest")
	}
	for name, checksum := range manifest.Checksums {
		if parsed[name] != checksum {
			return errors.New("checksum list does not match manifest")
		}
	}
	return nil
}
