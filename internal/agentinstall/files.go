package agentinstall

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func atomicWrite(path string, data []byte, mode os.FileMode) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".beat-agent-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() {
		_ = file.Close()
		if cleanupErr := os.Remove(temporary); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			err = errors.Join(err, cleanupErr)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func readPrivate(path string) ([]byte, error) {
	if err := checkFile(path, privateFile); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxPrivateFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPrivateFile {
		return nil, errors.New("private state or config exceeds the size limit")
	}
	return data, nil
}

func checkFile(path string, allowed os.FileMode) error {
	if err := checkAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !ownedByCurrentUser(info) || info.Mode().Perm()&^allowed != 0 {
		return fmt.Errorf("unsafe file %s: require a root-owned regular file with mode %04o or stricter", path, allowed)
	}
	return nil
}

func checkAncestors(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("installation paths must be clean absolute paths")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || !ownedByCurrentUser(info) || info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("unsafe directory %s: require root ownership and no untrusted writes or symlinks", current)
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func ensureDirectory(path string, private bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := ensureDirectory(filepath.Dir(path), false); err != nil {
			return err
		}
		if err := os.Mkdir(path, privateDirectory); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	if err != nil {
		return err
	}
	if err := checkAncestors(path); err != nil {
		return err
	}
	if private && info.Mode().Perm()&^os.FileMode(privateDirectory) != 0 {
		return fmt.Errorf("directory %s must have mode 0700 or stricter", path)
	}
	return nil
}

func probeDirectory(path string) error {
	file, err := os.CreateTemp(path, ".beat-agent-preflight-*")
	if err != nil {
		return err
	}
	if err := errors.Join(file.Sync(), file.Close(), os.Remove(file.Name())); err != nil {
		return err
	}
	return syncDirectory(path)
}
