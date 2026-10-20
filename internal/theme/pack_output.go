package theme

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/beat/backend/internal/model"
)

func manifestFile(name string, content []byte) model.ThemeManifestFile {
	return model.ThemeManifestFile{Path: name, Size: int64(len(content)), SHA256: sha256Hex(content)}
}

func WriteArchive(path string, content []byte) (err error) {
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return ErrArchiveInvalid
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".beat-theme-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if removeErr := os.Remove(file.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, removeErr)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
