package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const keyLength = 32

type Manager struct {
	aead   cipher.AEAD
	random io.Reader
	mu     sync.Mutex
}

func New(keyPath string, randomSource io.Reader) (*Manager, error) {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	key, err := loadOrCreateKey(keyPath, randomSource)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return &Manager{aead: aead, random: randomSource}, nil
}

func (manager *Manager) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, manager.aead.NonceSize())
	manager.mu.Lock()
	_, err := io.ReadFull(manager.random, nonce)
	manager.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("generate AES-GCM nonce: %w", err)
	}
	return manager.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (manager *Manager) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := manager.aead.NonceSize()
	if len(ciphertext) < nonceSize+manager.aead.Overhead() {
		return nil, errors.New("encrypted secret is invalid")
	}
	plaintext, err := manager.aead.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plaintext, nil
}

func loadOrCreateKey(path string, randomSource io.Reader) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != keyLength {
			return nil, errors.New("administrator data key must contain 32 bytes")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("secure administrator data key: %w", err)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read administrator data key: %w", err)
	}
	return createKey(path, randomSource)
}

func createKey(path string, randomSource io.Reader) ([]byte, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create administrator key directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("secure administrator key directory: %w", err)
	}
	key := make([]byte, keyLength)
	if _, err := io.ReadFull(randomSource, key); err != nil {
		return nil, fmt.Errorf("generate administrator data key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create administrator data key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write administrator data key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("sync administrator data key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close administrator data key: %w", err)
	}
	return key, nil
}
