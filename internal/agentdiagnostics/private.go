package agentdiagnostics

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	privateDirectoryMode = 0o700
	privateFileMode      = 0o600
)

// ensurePrivateDir 创建并复核私有状态目录；已存在的目录必须是 0700 且不是符号链接。
func ensurePrivateDir(path string) error {
	if path == "" {
		return ErrStorage
	}
	absolute, err := filepath.Abs(path)
	if err != nil || absolute != path {
		return ErrStorage
	}
	if err := os.MkdirAll(path, privateDirectoryMode); err != nil {
		return ErrStorage
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != privateDirectoryMode {
		return ErrStorage
	}
	if err := os.Chmod(path, privateDirectoryMode); err != nil {
		return ErrStorage
	}
	return nil
}

// writePrivateFile 原子写入 0600 文件；调用方负责在失败时保持旧内容。
func writePrivateFile(path string, content []byte) (resultErr error) {
	directory := filepath.Dir(path)
	if err := ensurePrivateDir(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".diagnostic-*.tmp")
	if err != nil {
		return ErrStorage
	}
	name := temporary.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(privateFileMode); err != nil {
		_ = temporary.Close()
		return ErrStorage
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return ErrStorage
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return ErrStorage
	}
	if err := temporary.Close(); err != nil {
		return ErrStorage
	}
	if err := os.Rename(name, path); err != nil {
		return ErrStorage
	}
	return nil
}

// readPrivateFile 只读取 0700 目录中的 0600 普通文件。
func readPrivateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, ErrStorage
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != privateFileMode {
		return nil, ErrStorage
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrStorage
	}
	return content, nil
}

func removePrivateFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrStorage
	}
	return nil
}
