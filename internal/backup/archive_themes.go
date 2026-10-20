package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/beat/backend/internal/model"
)

type themeArchiveCopy struct {
	source, target, digest string
	files                  map[string]model.BackupThemeFile
}

func (service *Service) copyThemes(temporary string, themeFiles map[string]model.BackupThemeFile) ([]string, error) {
	packages := filepath.Join(service.themesDir, "packages")
	entries, err := os.ReadDir(packages)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list theme packages for backup: %w", err)
	}
	payloads := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !isHexString(entry.Name(), 64) {
			continue
		}
		request := themeArchiveCopy{source: filepath.Join(packages, entry.Name()), target: filepath.Join(temporary, "themes", entry.Name()), digest: entry.Name(), files: themeFiles}
		copied, err := copyThemeArchiveTree(request)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, copied...)
	}
	sort.Strings(payloads)
	return payloads, nil
}

func copyThemeArchiveTree(request themeArchiveCopy) ([]string, error) {
	if err := os.MkdirAll(request.target, 0o700); err != nil {
		return nil, fmt.Errorf("create theme backup directory: %w", err)
	}
	payloads := make([]string, 0)
	err := filepath.WalkDir(request.source, func(path string, info os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(request.source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(request.target, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !info.Type().IsRegular() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writePrivateFile(target, content); err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		name := filepath.ToSlash(filepath.Join("themes", request.digest, relative))
		request.files[name] = model.BackupThemeFile{Path: name, Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:])}
		payloads = append(payloads, name)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("copy theme package %s for backup: %w", request.digest, err)
	}
	return payloads, nil
}

func writePrivateFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr, closeErr := file.Sync(), file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}
