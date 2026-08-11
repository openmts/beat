package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/beat/backend/internal/model"
)

type SSHKeyStore struct {
	db *sql.DB
}

func NewSSHKeyStore(db *sql.DB) *SSHKeyStore {
	return &SSHKeyStore{db: db}
}

func (s *SSHKeyStore) ListSSHKeys(ctx context.Context) ([]model.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, key_type, public_key, private_key, fingerprint, created_at FROM ssh_keys ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("querying ssh keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	keys := []model.SSHKey{}
	for rows.Next() {
		var k model.SSHKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyType, &k.PublicKey, &k.PrivateKey, &k.Fingerprint, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning ssh key: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating ssh keys: %w", err)
	}

	return keys, nil
}

func (s *SSHKeyStore) CreateSSHKey(ctx context.Context, name, keyType, publicKey, privateKey, fingerprint string) (*model.SSHKey, error) {
	now := model.NowUTC()
	k := &model.SSHKey{
		ID:          uuid.New().String(),
		Name:        name,
		KeyType:     keyType,
		PublicKey:   publicKey,
		PrivateKey:  privateKey,
		Fingerprint: fingerprint,
		CreatedAt:   now,
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO ssh_keys (id, name, key_type, public_key, private_key, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		k.ID, k.Name, k.KeyType, k.PublicKey, k.PrivateKey, k.Fingerprint, k.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting ssh key: %w", err)
	}

	return k, nil
}

func (s *SSHKeyStore) GetSSHKeyByPublicKey(ctx context.Context, publicKey string) (*model.SSHKey, error) {
	var key model.SSHKey
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, key_type, public_key, private_key, fingerprint, created_at FROM ssh_keys WHERE public_key = ?",
		publicKey,
	).Scan(
		&key.ID, &key.Name, &key.KeyType, &key.PublicKey,
		&key.PrivateKey, &key.Fingerprint, &key.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("querying ssh key by public key: %w", err)
	}

	return &key, nil
}

func (s *SSHKeyStore) DeleteSSHKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM ssh_keys WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("deleting ssh key: %w", err)
	}

	return nil
}
