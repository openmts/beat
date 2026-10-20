package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

// ErrSSHKeyNameConflict 表示同名 SSH 密钥已经存在；管理端据此回答 409 而不是 500。
var ErrSSHKeyNameConflict = errors.New("ssh key name already exists")

type SSHKeyStore struct {
	db      *sql.DB
	secrets secretbox.ResourceService
}

func (s *SSHKeyStore) SetSecrets(secrets secretbox.ResourceService) { s.secrets = secrets }

func NewSSHKeyStore(db *sql.DB) *SSHKeyStore {
	return &SSHKeyStore{db: db}
}

func (s *SSHKeyStore) ListSSHKeys(ctx context.Context) ([]model.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, key_type, public_key, fingerprint, created_at FROM ssh_keys ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("querying ssh keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := []model.SSHKey{}
	for rows.Next() {
		var k model.SSHKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyType, &k.PublicKey, &k.Fingerprint, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning ssh key: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating ssh keys: %w", err)
	}

	return keys, nil
}

func (s *SSHKeyStore) CreateSSHKey(ctx context.Context, input SSHKeyInput) (*model.SSHKey, error) {
	if s.secrets == nil {
		return nil, errors.New("SSH application secrets are unavailable")
	}
	now := model.NowUTC()
	k := &model.SSHKey{
		ID:          uuid.New().String(),
		Name:        input.Name,
		KeyType:     input.KeyType,
		PublicKey:   strings.TrimSpace(input.PublicKey),
		PrivateKey:  input.PrivateKey,
		Fingerprint: input.Fingerprint,
		CreatedAt:   now,
	}

	err := s.secrets.WithWrite(ctx, func(access secretbox.ResourceAccess) error {
		return s.insertEncryptedSSHKey(ctx, sshKeyWrite{key: k, generated: input.Generated}, access)
	})
	if err != nil {
		return nil, mapSSHKeyConstraintError("inserting ssh key", err)
	}

	return k, nil
}

func (s *SSHKeyStore) GetSSHKeyByPublicKey(ctx context.Context, publicKey string) (*model.SSHKey, error) {
	key, err := s.getSSHKeyByPublicKey(ctx, strings.TrimSpace(publicKey))
	if err != nil || key != nil {
		return key, err
	}
	// Fall back to a newline-terminated form for legacy rows stored with a
	// trailing newline before public key trimming was introduced.
	return s.getSSHKeyByPublicKey(ctx, strings.TrimSpace(publicKey)+"\n")
}

func (s *SSHKeyStore) getSSHKeyByPublicKey(ctx context.Context, publicKey string) (*model.SSHKey, error) {
	var key model.SSHKey
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, key_type, public_key, fingerprint, created_at FROM ssh_keys WHERE public_key = ?",
		publicKey,
	).Scan(
		&key.ID, &key.Name, &key.KeyType, &key.PublicKey,
		&key.Fingerprint, &key.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying ssh key by public key: %w", err)
	}

	return &key, nil
}

func (s *SSHKeyStore) GetSSHKeyByID(ctx context.Context, id string) (*model.SSHKey, error) {
	var key model.SSHKey
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, key_type, public_key, fingerprint, created_at FROM ssh_keys WHERE id = ?",
		id,
	).Scan(
		&key.ID, &key.Name, &key.KeyType, &key.PublicKey,
		&key.Fingerprint, &key.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying ssh key by id: %w", err)
	}

	return &key, nil
}

// mapSSHKeyConstraintError 把唯一约束冲突归类成可判定的管理端错误。
func mapSSHKeyConstraintError(operation string, err error) error {
	if strings.Contains(err.Error(), "ssh_keys.name") {
		return fmt.Errorf("%s: %w", operation, ErrSSHKeyNameConflict)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
