package store

import (
	"context"
	"errors"

	"github.com/beat/backend/internal/secretbox"
)

// ReadPrivateKey 只在已授权的连接或导出时读取选定的密文，不随普通元数据查询加载。
func (s *SSHKeyStore) ReadPrivateKey(ctx context.Context, id string) (string, error) {
	if s.secrets == nil {
		return "", errors.New("SSH application secrets are unavailable")
	}
	var ciphertext []byte
	if err := s.db.QueryRowContext(ctx, "SELECT private_key_encrypted FROM ssh_keys WHERE id = ?", id).Scan(&ciphertext); err != nil {
		return "", err
	}
	if len(ciphertext) == 0 {
		return "", s.authorizeSSHKeyRead(ctx, id, ciphertext)
	}
	plaintext, err := s.secrets.OpenResource(ctx, ciphertext, secretbox.SSHPrivateKeyAAD(id))
	if err != nil {
		return "", err
	}
	defer clear(plaintext)
	if err := s.authorizeSSHKeyRead(ctx, id, ciphertext); err != nil {
		return "", err
	}
	return string(plaintext), nil
}
