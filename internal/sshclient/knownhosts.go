package sshclient

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type KnownHosts struct {
	path string
	mu   sync.Mutex
}

func NewKnownHosts(path string) (*KnownHosts, error) {
	if path == "" {
		return nil, errors.New("known hosts path is required")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create known hosts directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure known hosts directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create known hosts file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure known hosts file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close known hosts file: %w", err)
	}

	return &KnownHosts{path: path}, nil
}

func (k *KnownHosts) Callback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	verify, err := knownhosts.New(k.path)
	if err != nil {
		return fmt.Errorf("load known hosts: %w", err)
	}
	if err := verify(hostname, remote, key); err == nil {
		return nil
	} else if !isUnknownHost(err) {
		return fmt.Errorf("verify host key: %w", err)
	}

	if err := k.append(hostname, key); err != nil {
		return err
	}
	return nil
}

func isUnknownHost(err error) bool {
	var keyError *knownhosts.KeyError
	return errors.As(err, &keyError) && len(keyError.Want) == 0
}

func (k *KnownHosts) append(hostname string, key ssh.PublicKey) error {
	file, err := os.OpenFile(k.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known hosts: %w", err)
	}

	line := knownhosts.Line([]string{hostname}, key) + "\n"
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		return fmt.Errorf("write known host: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close known hosts: %w", err)
	}
	return nil
}
