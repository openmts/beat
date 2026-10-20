package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/beat/backend/internal/model"
	"github.com/beat/backend/internal/secretbox"
)

type SSHKeyInput struct {
	Name, KeyType, PublicKey, PrivateKey, Fingerprint string
	Generated                                         bool
}

func (SSHKeyInput) String() string { return "SSHKeyInput{redacted}" }

func (input SSHKeyInput) GoString() string { return input.String() }

type sshKeyWrite struct {
	key       *model.SSHKey
	generated bool
}

func (s *SSHKeyStore) insertEncryptedSSHKey(ctx context.Context, input sshKeyWrite, access secretbox.ResourceAccess) error {
	key := input.key
	var ciphertext []byte
	if key.PrivateKey != "" {
		plaintext := []byte(key.PrivateKey)
		defer clear(plaintext)
		var err error
		ciphertext, err = access.Seal(plaintext, secretbox.SSHPrivateKeyAAD(key.ID))
		if err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`INSERT INTO ssh_keys (id, name, key_type, public_key, private_key_encrypted, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.Name, key.KeyType, key.PublicKey, ciphertext, key.Fingerprint, key.CreatedAt)
	if err := oneAdministratorWrite(result, err); err != nil {
		return err
	}
	action := "ssh_key.import"
	if input.generated {
		action = "ssh_key.generate"
	}
	if err := mutation.finish(metadataEvidence{Action: action, Type: "ssh_key", ID: key.ID, Name: key.Name, Fields: []string{"key"}}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SSHKeyStore) DeleteSSHKey(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	mutation, err := beginMetadataMutation(ctx, tx, true)
	if err != nil {
		return err
	}
	var name string
	err = tx.QueryRowContext(ctx, "SELECT name FROM ssh_keys WHERE id=?", id).Scan(&name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM ssh_keys WHERE id=?", id); err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM ssh_keys WHERE id=?", id).Scan(&remaining); err != nil {
		return err
	}
	if remaining != 0 {
		return ErrAdminMutationUnavailable
	}
	if err := mutation.finish(metadataEvidence{Action: "ssh_key.delete", Type: "ssh_key", ID: id, Name: name, Fields: []string{"deleted"}}); err != nil {
		return err
	}
	return tx.Commit()
}
