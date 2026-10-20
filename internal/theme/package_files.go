package theme

import (
	"fmt"
	"os"
	"path/filepath"
)

// stageArchive 将校验后的归档写入暂存目录（archive.zip + content/*）。
func (service *Service) stageArchive(staging string, validated *ValidatedPackage, raw []byte) error {
	if err := writePrivate(filepath.Join(staging, themeOwnershipMarker), []byte(validated.Digest)); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(staging, "archive.zip"), raw); err != nil {
		return err
	}
	contentDir := filepath.Join(staging, "content")
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		return fmt.Errorf("create theme content directory: %w", err)
	}
	for _, file := range validated.Files {
		content := validated.Content[file.Path]
		destination := filepath.Join(contentDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return fmt.Errorf("create theme content parent: %w", err)
		}
		if err := writePrivate(destination, content); err != nil {
			return err
		}
	}
	return syncStagedTheme(staging)
}

// writePrivate 以 0600 权限写入文件。
func writePrivate(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create theme file: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return fmt.Errorf("write theme file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync theme file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close theme file: %w", err)
	}
	return nil
}
